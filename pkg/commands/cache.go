package commands

import (
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/mattmoor/mink/pkg/bundles/kontext"
	"github.com/spf13/cobra"
	"github.com/wolfi-dev/dag/pkg"
	"gopkg.in/yaml.v2"
)

func cmdCache() *cobra.Command {
	var dir, out, repo string
	cache := &cobra.Command{
		Use:   "cache",
		Short: "Fetch and cache remote dependencies of a directory of packages",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			start := time.Now()

			var cfgs []pkg.Config
			if err := filepath.Walk(dir, func(path string, info fs.FileInfo, err error) error {
				if err != nil {
					return err
				}
				// Don't walk into directories.
				if path != dir && info.IsDir() {
					return filepath.SkipDir
				}
				if strings.HasSuffix(path, ".yaml") {
					f, err := os.Open(path)
					if err != nil {
						return err
					}
					defer f.Close()
					var c pkg.Config
					if err := yaml.NewDecoder(f).Decode(&c); err != nil {
						return err
					}
					cfgs = append(cfgs, c)
				}
				return nil
			}); err != nil {
				return err
			}

			if err := os.MkdirAll(out, 0755); err != nil {
				return err
			}

			var count int
			var size uint64
			for _, cfg := range cfgs {
				if ctx.Err() != nil { // Check for cancellation.
					return ctx.Err()
				}
				for _, uri := range cfg.URIs() {
					count++
					// TODO: reuse the code melange uses to apply replacements.
					uri.URI = strings.ReplaceAll(uri.URI, "${{package.name}}", cfg.Package.Name)
					uri.URI = strings.ReplaceAll(uri.URI, "${{package.version}}", cfg.Package.Version)

					var h hash.Hash
					var fn, want string
					if uri.ExpectedSHA256 != "" {
						h = sha256.New()
						fn = fmt.Sprintf("sha256:%s", uri.ExpectedSHA256)
						want = uri.ExpectedSHA256
					} else if uri.ExpectedSHA512 != "" {
						h = sha512.New()
						fn = fmt.Sprintf("sha512:%s", uri.ExpectedSHA512)
						want = uri.ExpectedSHA512
					} else {
						return fmt.Errorf("invalid checksum provided for %s", uri.URI)
					}
					fn = filepath.Join(out, fn)

					// If the file already exists locally, check the hash and maybe skip download.
					if _, err := os.Stat(fn); err == nil {
						f, err := os.Open(fn)
						if err != nil {
							return err
						}
						if _, err := io.Copy(h, f); err != nil {
							return err
						}
						got := fmt.Sprintf("%x", h.Sum(nil))
						if got == want {
							log.Printf("Caching %s: found %s already in cache", uri.URI, fn)
							continue
						}
						log.Printf("Caching %s: found %s in cache with hash mismatch (got %s), redownloading", uri.URI, fn, got)
						h.Reset()
					}

					f, err := os.Create(fn)
					if err != nil {
						return err
					}
					defer f.Close()

					req, err := http.NewRequest(http.MethodGet, uri.URI, nil)
					if err != nil {
						return err
					}
					req = req.WithContext(ctx)
					resp, err := http.DefaultClient.Do(req)
					if err != nil {
						return fmt.Errorf("fetching %s: %w", uri.URI, err)
					}
					defer resp.Body.Close()

					log.Printf("Caching %s -> %s", uri.URI, fn)
					cw := &countWriter{}
					if _, err := io.Copy(io.MultiWriter(f, h, cw), resp.Body); err != nil {
						return err
					}
					got := fmt.Sprintf("%x", h.Sum(nil))
					if got != want {
						log.Println("!!!!!!!!!")
						log.Printf("CHECKSUM MISMATCH %s: got %s, want %s", uri.URI, got, want)
						log.Println("!!!!!!!!!")
						// TODO: make this an error
					}

					size += cw.n
				}
			}
			log.Printf("Cached %d URIs (%s) in %s", count, humanize.Bytes(size), time.Since(start))

			if repo != "" {
				// Bundle the cache dir into an image.
				t, err := name.NewTag(repo, name.WeakValidation)
				if err != nil {
					return err
				}
				dig, err := kontext.Bundle(ctx, out, t)
				if err != nil {
					return err
				}
				log.Println("bundled source context to", dig)
			}
			return nil
		},
	}
	cache.Flags().StringVarP(&dir, "dir", "d", ".", "directory to search for melange configs")
	cache.Flags().StringVarP(&out, "out", "o", "./cache", "output directory")
	cache.Flags().StringVar(&repo, "repo", "", "if set, OCI repository to push the bundle to")
	return cache
}

// countWriter counts bytes written to it.
type countWriter struct{ n uint64 }

func (c *countWriter) Write(p []byte) (int, error) {
	c.n += uint64(len(p))
	return len(p), nil
}
