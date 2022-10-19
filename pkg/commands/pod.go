package commands

import (
	"bytes"
	"fmt"
	"log"
	"os"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/mattmoor/mink/pkg/bundles/kontext"
	"github.com/spf13/cobra"
	"github.com/wolfi-dev/dag/pkg/graph"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
)

func cmdPod() *cobra.Command {
	var dir, arch, repo string
	pod := &cobra.Command{
		Use:   "pod",
		Short: "Generate a kubernetes pod to run the build",
		Args:  cobra.MinimumNArgs(1), // TODO(jason): Support no roots; full graph
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			// TODO(jason): Accept and translate --arch=arm64 -> aarch64, etc.

			g := graph.New()
			if err := g.Walk(dir); err != nil {
				return err
			}
			if err := g.Validate(); err != nil {
				return err
			}

			var buf bytes.Buffer
			text(g, args, arch, &buf)

			// Bundle the source context into an image.
			t, err := name.NewTag(repo, name.WeakValidation)
			if err != nil {
				return err
			}
			dig, err := kontext.Bundle(ctx, dir, t)
			if err != nil {
				return err
			}
			log.Println("bundled source context to", dig)

			p := &corev1.Pod{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Pod",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "dag-",
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					InitContainers: []corev1.Container{{
						Name:  "init",
						Image: dig.String(),
					}},
					Containers: []corev1.Container{{
						Name:  "build",
						Image: "cgr.dev/chainguard/sdk",
						Command: []string{
							"bash", "-c",
							fmt.Sprintf(`
set -euo pipefail
cd /var/run/kontext/
ls -lhR
echo "=== TO BUILD ==="
echo %q`, buf.String()),
						},
					}},
				},
			}

			// https://stackoverflow.com/questions/35035464/generate-yaml-manifest-from-kubernetes-types
			return json.NewYAMLSerializer(json.DefaultMetaFactory, nil, nil).Encode(p, os.Stdout)
		},
	}
	pod.Flags().StringVarP(&dir, "dir", "d", ".", "directory to search for melange configs")
	pod.Flags().StringVarP(&arch, "arch", "a", "x86_64", "architecture to build for")
	pod.Flags().StringVar(&repo, "repo", "", "repository to push the bundle to")
	pod.MarkFlagRequired("repo")
	return pod
}
