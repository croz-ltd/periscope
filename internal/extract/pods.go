package extract

import (
	"context"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/croz-ltd/periscope/internal/model"
)

// Pods counts the pods of the whole cluster, in all namespaces: the total, the
// pods in phase Running, and the pods with a container in CrashLoopBackOff.
// The counts are informational, because clusters differ in size by design.
//
// A crash-looping pod usually stays in phase Running, so it is also part of the
// running count. The total row carries the count per phase in its tooltip.
type Pods struct{}

func (Pods) Key() string { return "pods" }

// podPageSize keeps one list call small. A large cluster runs tens of
// thousands of pods, and one unpaged list of all of them is a heavy request for
// the API server and a large response for the hub to hold.
const podPageSize = 500

func (Pods) Extract(ctx context.Context, c *Clients) ([]model.Component, error) {
	total, running, crashLooping := 0, 0, 0
	byPhase := map[string]int{}

	opts := metav1.ListOptions{Limit: podPageSize}
	for {
		page, err := c.Typed.CoreV1().Pods(metav1.NamespaceAll).List(ctx, opts)
		if err != nil {
			return nil, err
		}
		for i := range page.Items {
			p := &page.Items[i]
			total++
			phase := string(p.Status.Phase)
			if phase == "" {
				phase = "Unknown"
			}
			byPhase[phase]++
			if p.Status.Phase == corev1.PodRunning {
				running++
			}
			if isCrashLooping(p) {
				crashLooping++
			}
		}
		if page.Continue == "" {
			break
		}
		opts.Continue = page.Continue
	}

	extra := make(map[string]string, len(byPhase))
	for phase, n := range byPhase {
		extra[phase] = strconv.Itoa(n)
	}
	info := func(key, name string, count int, ex map[string]string) model.Component {
		return model.Component{
			Key: key, Name: name, Group: model.GroupWorkloads,
			Compare: model.CompareInfo, Kind: "pods",
			Version: strconv.Itoa(count), Extra: ex,
		}
	}
	return []model.Component{
		info("pod-total", "Pods total", total, extra),
		info("pod-running", "Pods running", running, nil),
		info("pod-crashloop", "Pods crash-looping", crashLooping, nil),
	}, nil
}

// isCrashLooping reports whether any container of the pod, init containers
// included, waits in CrashLoopBackOff.
func isCrashLooping(p *corev1.Pod) bool {
	for _, list := range [][]corev1.ContainerStatus{p.Status.InitContainerStatuses, p.Status.ContainerStatuses} {
		for _, s := range list {
			if s.State.Waiting != nil && s.State.Waiting.Reason == "CrashLoopBackOff" {
				return true
			}
		}
	}
	return false
}
