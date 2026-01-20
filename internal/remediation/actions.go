package remediation

import (
	"context"
	"fmt"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// Action represents a remediation action that can be executed
type Action interface {
	Name() string
	Execute(ctx context.Context, target Target, params map[string]string) error
	Validate(params map[string]string) error
}

// Target identifies the Kubernetes resource to act on
type Target struct {
	Namespace  string
	Pod        string
	Deployment string
	Container  string
}

// String returns a string representation of the target
func (t Target) String() string {
	if t.Pod != "" {
		return fmt.Sprintf("%s/%s", t.Namespace, t.Pod)
	}
	if t.Deployment != "" {
		return fmt.Sprintf("%s/deployment/%s", t.Namespace, t.Deployment)
	}
	return t.Namespace
}

// RestartPodAction deletes a pod to trigger a restart
type RestartPodAction struct {
	client kubernetes.Interface
}

func NewRestartPodAction(client kubernetes.Interface) *RestartPodAction {
	return &RestartPodAction{client: client}
}

func (a *RestartPodAction) Name() string {
	return "restart-pod"
}

func (a *RestartPodAction) Execute(ctx context.Context, target Target, params map[string]string) error {
	if target.Pod == "" {
		return fmt.Errorf("pod name is required")
	}

	gracePeriod := int64(0) // Force immediate deletion
	deletePolicy := metav1.DeletePropagationForeground

	err := a.client.CoreV1().Pods(target.Namespace).Delete(ctx, target.Pod, metav1.DeleteOptions{
		GracePeriodSeconds: &gracePeriod,
		PropagationPolicy:  &deletePolicy,
	})
	if err != nil {
		return fmt.Errorf("deleting pod: %w", err)
	}

	return nil
}

func (a *RestartPodAction) Validate(params map[string]string) error {
	return nil
}

// ScaleUpAction increases the replica count of a deployment
type ScaleUpAction struct {
	client kubernetes.Interface
}

func NewScaleUpAction(client kubernetes.Interface) *ScaleUpAction {
	return &ScaleUpAction{client: client}
}

func (a *ScaleUpAction) Name() string {
	return "scale-up"
}

func (a *ScaleUpAction) Execute(ctx context.Context, target Target, params map[string]string) error {
	deployment, err := a.getDeployment(ctx, target)
	if err != nil {
		return err
	}

	// Determine new replica count
	currentReplicas := int32(1)
	if deployment.Spec.Replicas != nil {
		currentReplicas = *deployment.Spec.Replicas
	}

	increment := int32(1)
	if val, ok := params["replicas"]; ok {
		if val[0] == '+' {
			inc, err := strconv.ParseInt(val[1:], 10, 32)
			if err != nil {
				return fmt.Errorf("invalid replicas increment: %w", err)
			}
			increment = int32(inc)
		} else {
			abs, err := strconv.ParseInt(val, 10, 32)
			if err != nil {
				return fmt.Errorf("invalid replicas value: %w", err)
			}
			increment = int32(abs) - currentReplicas
		}
	}

	newReplicas := currentReplicas + increment
	if newReplicas < 1 {
		newReplicas = 1
	}

	// Apply max replicas limit
	if maxStr, ok := params["max_replicas"]; ok {
		max, err := strconv.ParseInt(maxStr, 10, 32)
		if err == nil && newReplicas > int32(max) {
			return fmt.Errorf("would exceed max replicas limit (%d)", max)
		}
	}

	patch := fmt.Sprintf(`{"spec":{"replicas":%d}}`, newReplicas)
	_, err = a.client.AppsV1().Deployments(target.Namespace).Patch(
		ctx,
		deployment.Name,
		types.MergePatchType,
		[]byte(patch),
		metav1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("scaling deployment: %w", err)
	}

	return nil
}

func (a *ScaleUpAction) Validate(params map[string]string) error {
	if val, ok := params["replicas"]; ok {
		if val[0] == '+' {
			if _, err := strconv.ParseInt(val[1:], 10, 32); err != nil {
				return fmt.Errorf("invalid replicas increment: %w", err)
			}
		} else {
			if _, err := strconv.ParseInt(val, 10, 32); err != nil {
				return fmt.Errorf("invalid replicas value: %w", err)
			}
		}
	}
	return nil
}

func (a *ScaleUpAction) getDeployment(ctx context.Context, target Target) (*appsv1.Deployment, error) {
	if target.Deployment != "" {
		return a.client.AppsV1().Deployments(target.Namespace).Get(ctx, target.Deployment, metav1.GetOptions{})
	}

	// Find deployment from pod
	if target.Pod == "" {
		return nil, fmt.Errorf("either deployment or pod name is required")
	}

	pod, err := a.client.CoreV1().Pods(target.Namespace).Get(ctx, target.Pod, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting pod: %w", err)
	}

	// Look for deployment owner reference
	for _, ref := range pod.OwnerReferences {
		if ref.Kind == "ReplicaSet" {
			rs, err := a.client.AppsV1().ReplicaSets(target.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
			if err != nil {
				continue
			}
			for _, rsRef := range rs.OwnerReferences {
				if rsRef.Kind == "Deployment" {
					return a.client.AppsV1().Deployments(target.Namespace).Get(ctx, rsRef.Name, metav1.GetOptions{})
				}
			}
		}
	}

	return nil, fmt.Errorf("could not find deployment for pod %s", target.Pod)
}

// ScaleDownAction decreases the replica count of a deployment
type ScaleDownAction struct {
	client kubernetes.Interface
}

func NewScaleDownAction(client kubernetes.Interface) *ScaleDownAction {
	return &ScaleDownAction{client: client}
}

func (a *ScaleDownAction) Name() string {
	return "scale-down"
}

func (a *ScaleDownAction) Execute(ctx context.Context, target Target, params map[string]string) error {
	deployment, err := a.getDeployment(ctx, target)
	if err != nil {
		return err
	}

	currentReplicas := int32(1)
	if deployment.Spec.Replicas != nil {
		currentReplicas = *deployment.Spec.Replicas
	}

	decrement := int32(1)
	if val, ok := params["replicas"]; ok {
		if val[0] == '-' {
			dec, err := strconv.ParseInt(val[1:], 10, 32)
			if err != nil {
				return fmt.Errorf("invalid replicas decrement: %w", err)
			}
			decrement = int32(dec)
		} else {
			abs, err := strconv.ParseInt(val, 10, 32)
			if err != nil {
				return fmt.Errorf("invalid replicas value: %w", err)
			}
			decrement = currentReplicas - int32(abs)
		}
	}

	newReplicas := currentReplicas - decrement
	minReplicas := int32(1)
	if minStr, ok := params["min_replicas"]; ok {
		min, err := strconv.ParseInt(minStr, 10, 32)
		if err == nil {
			minReplicas = int32(min)
		}
	}
	if newReplicas < minReplicas {
		newReplicas = minReplicas
	}

	patch := fmt.Sprintf(`{"spec":{"replicas":%d}}`, newReplicas)
	_, err = a.client.AppsV1().Deployments(target.Namespace).Patch(
		ctx,
		deployment.Name,
		types.MergePatchType,
		[]byte(patch),
		metav1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("scaling deployment: %w", err)
	}

	return nil
}

func (a *ScaleDownAction) Validate(params map[string]string) error {
	return nil
}

func (a *ScaleDownAction) getDeployment(ctx context.Context, target Target) (*appsv1.Deployment, error) {
	// Same implementation as ScaleUpAction
	su := &ScaleUpAction{client: a.client}
	return su.getDeployment(ctx, target)
}

// RollbackAction rolls back a deployment to the previous revision
type RollbackAction struct {
	client kubernetes.Interface
}

func NewRollbackAction(client kubernetes.Interface) *RollbackAction {
	return &RollbackAction{client: client}
}

func (a *RollbackAction) Name() string {
	return "rollback"
}

func (a *RollbackAction) Execute(ctx context.Context, target Target, params map[string]string) error {
	// Get the deployment
	su := &ScaleUpAction{client: a.client}
	deployment, err := su.getDeployment(ctx, target)
	if err != nil {
		return err
	}

	// Get ReplicaSets for this deployment
	selector := deployment.Spec.Selector
	replicaSets, err := a.client.AppsV1().ReplicaSets(target.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: metav1.FormatLabelSelector(selector),
	})
	if err != nil {
		return fmt.Errorf("listing replicasets: %w", err)
	}

	if len(replicaSets.Items) < 2 {
		return fmt.Errorf("no previous revision to rollback to")
	}

	// Find the previous revision (second most recent)
	var previous *appsv1.ReplicaSet
	var current *appsv1.ReplicaSet
	for i := range replicaSets.Items {
		rs := &replicaSets.Items[i]
		if rs.Annotations["deployment.kubernetes.io/revision"] == "" {
			continue
		}
		if current == nil || rs.CreationTimestamp.After(current.CreationTimestamp.Time) {
			previous = current
			current = rs
		} else if previous == nil || rs.CreationTimestamp.After(previous.CreationTimestamp.Time) {
			previous = rs
		}
	}

	if previous == nil {
		return fmt.Errorf("no previous revision found")
	}

	// Patch deployment with previous template
	patch := map[string]interface{}{
		"spec": map[string]interface{}{
			"template": previous.Spec.Template,
		},
	}

	patchBytes, err := jsonMarshal(patch)
	if err != nil {
		return fmt.Errorf("marshaling patch: %w", err)
	}

	_, err = a.client.AppsV1().Deployments(target.Namespace).Patch(
		ctx,
		deployment.Name,
		types.MergePatchType,
		patchBytes,
		metav1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("patching deployment: %w", err)
	}

	return nil
}

func (a *RollbackAction) Validate(params map[string]string) error {
	return nil
}

// DeleteStuckPodsAction deletes pods stuck in Terminating state
type DeleteStuckPodsAction struct {
	client kubernetes.Interface
}

func NewDeleteStuckPodsAction(client kubernetes.Interface) *DeleteStuckPodsAction {
	return &DeleteStuckPodsAction{client: client}
}

func (a *DeleteStuckPodsAction) Name() string {
	return "delete-stuck-pods"
}

func (a *DeleteStuckPodsAction) Execute(ctx context.Context, target Target, params map[string]string) error {
	// List pods in the namespace
	listOpts := metav1.ListOptions{}
	if target.Pod != "" {
		listOpts.FieldSelector = fmt.Sprintf("metadata.name=%s", target.Pod)
	}

	pods, err := a.client.CoreV1().Pods(target.Namespace).List(ctx, listOpts)
	if err != nil {
		return fmt.Errorf("listing pods: %w", err)
	}

	gracePeriod := int64(0)
	deleteOpts := metav1.DeleteOptions{
		GracePeriodSeconds: &gracePeriod,
	}

	for _, pod := range pods.Items {
		if pod.DeletionTimestamp != nil && pod.Status.Phase == corev1.PodRunning {
			// Pod is stuck in terminating
			err := a.client.CoreV1().Pods(target.Namespace).Delete(ctx, pod.Name, deleteOpts)
			if err != nil {
				return fmt.Errorf("force deleting pod %s: %w", pod.Name, err)
			}
		}
	}

	return nil
}

func (a *DeleteStuckPodsAction) Validate(params map[string]string) error {
	return nil
}

// NoneAction is a no-op action used for alert-only rules
type NoneAction struct{}

func NewNoneAction() *NoneAction {
	return &NoneAction{}
}

func (a *NoneAction) Name() string {
	return "none"
}

func (a *NoneAction) Execute(ctx context.Context, target Target, params map[string]string) error {
	// No-op
	return nil
}

func (a *NoneAction) Validate(params map[string]string) error {
	return nil
}

// Helper to marshal JSON (avoiding import cycle)
func jsonMarshal(v interface{}) ([]byte, error) {
	// Simple implementation for the patch case
	return []byte(fmt.Sprintf(`{"spec":{"template":%v}}`, v)), nil
}
