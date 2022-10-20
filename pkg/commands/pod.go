package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/mattmoor/mink/pkg/bundles/kontext"
	"github.com/spf13/cobra"
	"github.com/wolfi-dev/dag/pkg/graph"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/pointer"
)

func cmdPod() *cobra.Command {
	var dir, arch, repo, ns, cpu, ram, sa, sdkimg string
	var create, watch bool
	pod := &cobra.Command{
		Use:   "pod",
		Short: "Generate a kubernetes pod to run the build",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Don't use cmd.Context() since we want to capture signals to kill the pod.
			ctx := context.Background()

			if arch == "arm64" {
				arch = "aarch64"
			}

			targets := []string{"all"}
			if len(args) > 0 {
				g := graph.New()
				if err := g.Walk(dir); err != nil {
					return err
				}
				if err := g.Validate(); err != nil {
					return err
				}
				pkgs := list(g, args)
				targets = nil
				for _, p := range pkgs {
					targets = append(targets, g.Package(p).MakeTarget(arch))
				}
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

			p := &corev1.Pod{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Pod",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "dag-",
					Namespace:    ns,
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
								corev1.ResourceCPU:              resource.MustParse("1"),
								corev1.ResourceMemory:           resource.MustParse("2Gi"),
								corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
							},
						},
					}},
					Containers: []corev1.Container{{
						Name:       "build",
						Image:      sdkimg,
						WorkingDir: "/workspace",
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "workspace",
							MountPath: "/workspace",
						}},
						SecurityContext: &corev1.SecurityContext{
							Privileged: pointer.Bool(true),
						},
						Command: []string{
							"sh", "-c",
							fmt.Sprintf(`
set -euo pipefail
MELANGE=/usr/bin/melange MELANGE_DIR=/usr/share/melange make local-melange.rsa
MELANGE=/usr/bin/melange MELANGE_DIR=/usr/share/melange make %s`, strings.Join(targets, " ")),
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:              resource.MustParse(cpu),
								corev1.ResourceMemory:           resource.MustParse(ram),
								corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
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
					//"cloud.google.com/compute-class": "Scale-Out", TODO(jason): Needed for GKE Autopilot.
					"kubernetes.io/arch": "arm64",
				}
			}

			if create {
				k8s, err := newK8s()
				if err != nil {
					return err
				}
				p, err = k8s.create(ctx, p)
				if err != nil {
					return err
				}
				log.Println("created pod:", p.Name)
				if watch {
					return k8s.watch(ctx, p)
				}
				return nil
			}

			return json.NewYAMLSerializer(json.DefaultMetaFactory, nil, nil).Encode(p, os.Stdout)
		},
	}
	pod.Flags().StringVarP(&dir, "dir", "d", ".", "directory to search for melange configs")
	pod.Flags().StringVarP(&arch, "arch", "a", "x86_64", "architecture to build for")
	pod.Flags().StringVar(&repo, "repo", "", "repository to push the bundle to")
	pod.Flags().StringVarP(&ns, "namespace", "n", "default", "namespace to create the pod in")
	pod.Flags().StringVar(&cpu, "cpu", "1", "CPU request")
	pod.Flags().StringVar(&ram, "ram", "2Gi", "RAM request")
	pod.Flags().StringVar(&sa, "service-account", "default", "service account to use")
	pod.Flags().BoolVar(&create, "create", true, "create the pod")
	pod.Flags().BoolVarP(&watch, "watch", "w", true, "watch the pod, stream logs")
	pod.Flags().StringVar(&sdkimg, "sdk-image", "cgr.dev/chainguard/sdk:latest", "sdk image to use") // alpine-based, but supports arm64
	pod.MarkFlagRequired("repo")
	return pod
}

type k8s struct {
	clientset kubernetes.Clientset
}

func newK8s() (*k8s, error) {
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return &k8s{clientset: *clientset}, nil
}

func (k *k8s) create(ctx context.Context, p *corev1.Pod) (*corev1.Pod, error) {
	return k.clientset.CoreV1().Pods(p.Namespace).Create(ctx, p, metav1.CreateOptions{})
}

func (k *k8s) watch(ctx context.Context, p *corev1.Pod) error {
	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-c:
			log.Println("interrupted, deleting pod")
			if err := k.clientset.CoreV1().Pods(p.Namespace).Delete(context.Background(), p.Name, metav1.DeleteOptions{}); err != nil {
				log.Println("failed to delete pod:", err)
			}
			// TODO: Wait for pod to be deleted.
			log.Println("deleted pod: ", p.Name)
			os.Exit(1)
		case <-ctx.Done():
			return
		}
	}()

	w, err := k.clientset.CoreV1().Pods(p.Namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: "metadata.name=" + p.Name,
	})
	if err != nil {
		return err
	}

	for {
		select {
		case e := <-w.ResultChan():
			var ok bool
			p, ok = e.Object.(*corev1.Pod)
			if !ok {
				if st, ok := e.Object.(*metav1.Status); ok {
					log.Println("saw watch update with status:", st.Message)
					continue
				}
				return fmt.Errorf("unexpected object type: %T", e.Object)
			}
			switch p.Status.Phase {
			case corev1.PodPending:
				s := p.Status
				if len(s.InitContainerStatuses) > 0 && s.InitContainerStatuses[0].State.Running != nil {
					log.Println("init container running")
					continue
				}
				if len(s.ContainerStatuses) > 0 && s.ContainerStatuses[0].State.Waiting != nil {
					log.Println("build container waiting:", p.Status.ContainerStatuses[0].State.Waiting.Reason)
					continue
				}
				log.Println("pending...")
				time.Sleep(time.Second)
			case corev1.PodRunning:
				log.Printf("running... took %s", time.Now().Sub(p.CreationTimestamp.Time))

				// Start streaming logs.
				errCh := make(chan error)
				go func() {
					defer close(errCh)
					rc, err := k.clientset.CoreV1().Pods(p.Namespace).GetLogs(p.Name, &corev1.PodLogOptions{
						Container: "build",
						Follow:    true,
					}).Stream(ctx)
					if err != nil {
						errCh <- err
						return
					}
					defer rc.Close()
					if _, err := io.Copy(os.Stdout, rc); err != nil {
						errCh <- err
						return
					}
				}()
				return <-errCh
			case corev1.PodSucceeded:
				log.Printf("succeeded! took %s", time.Now().Sub(p.CreationTimestamp.Time))
				return nil
			case corev1.PodFailed:
				log.Println("failed!")
				s := p.Status.ContainerStatuses[0].State.Terminated.Message
				return fmt.Errorf("pod failed: %s", s)
			case corev1.PodUnknown:
				log.Println("unknown status...")
			default:
				return fmt.Errorf("unknown phase: %s", p.Status.Phase)
			}
		}
	}
	return errors.New("watch channel closed")
}
