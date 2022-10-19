package commands

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/mattmoor/mink/pkg/bundles/kontext"
	"github.com/spf13/cobra"
	"github.com/wolfi-dev/dag/pkg/graph"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
)

func cmdPod() *cobra.Command {
	var dir, arch, repo, cpu, ram, sa string
	pod := &cobra.Command{
		Use:   "pod",
		Short: "Generate a kubernetes pod to run the build",
		Args:  cobra.MinimumNArgs(1), // TODO(jason): Support no roots; full graph
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			if arch == "arm64" {
				arch = "aarch64"
			}

			g := graph.New()
			if err := g.Walk(dir); err != nil {
				return err
			}
			if err := g.Validate(); err != nil {
				return err
			}

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

			var targets []string
			pkgs := list(g, args)
			for _, p := range pkgs {
				targets = append(targets, g.Package(p).MakeTarget(arch))
			}

			p := &corev1.Pod{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Pod",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "dag-",
				},
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: sa,
					InitContainers: []corev1.Container{{
						Name:       "init",
						Image:      dig.String(),
						WorkingDir: "/workspace",
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "workspace",
							MountPath: "/workspace",
						}},
						Resources: corev1.ResourceRequirements{
							// Minimums required by Autopilot.
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("1"),
								corev1.ResourceMemory: resource.MustParse("1Gi"),
							},
						},
					}},
					Containers: []corev1.Container{{
						Name:       "build",
						Image:      "cgr.dev/chainguard/sdk",
						WorkingDir: "/workspace",
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "workspace",
							MountPath: "/workspace",
						}},
						Command: []string{
							"bash", "-c",
							fmt.Sprintf(`
set -euo pipefail
MELANGE=/usr/bin/melange MELANGE_DIR=/usr/share/melange make local-melange.rsa
MELANGE=/usr/bin/melange MELANGE_DIR=/usr/share/melange make %s`, strings.Join(targets, " ")),
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse(cpu),
								corev1.ResourceMemory: resource.MustParse(ram),
							},
						},
					}},
					Volumes: []corev1.Volume{{
						Name: "workspace",
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					}},
				},
			}

			if arch == "aarch64" {
				p.Spec.NodeSelector = map[string]string{
					"cloud.google.com/compute-class": "Scale-Out",
					"kubernetes.io/arch":             "arm64",
				}
			}

			return json.NewYAMLSerializer(json.DefaultMetaFactory, nil, nil).Encode(p, os.Stdout)
		},
	}
	pod.Flags().StringVarP(&dir, "dir", "d", ".", "directory to search for melange configs")
	pod.Flags().StringVarP(&arch, "arch", "a", "x86_64", "architecture to build for")
	pod.Flags().StringVar(&repo, "repo", "", "repository to push the bundle to")
	pod.Flags().StringVar(&cpu, "cpu", "1", "CPU request")
	pod.Flags().StringVar(&ram, "ram", "2Gi", "RAM request")
	pod.Flags().StringVar(&sa, "service-account", "default", "service account to use")
	pod.MarkFlagRequired("repo")
	return pod
}
