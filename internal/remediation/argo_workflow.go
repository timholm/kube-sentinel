package remediation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"text/template"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// ArgoWorkflowAction triggers an Argo Workflow for remediation
type ArgoWorkflowAction struct {
	client    dynamic.Interface
	namespace string
}

// NewArgoWorkflowAction creates a new Argo Workflow action
func NewArgoWorkflowAction(client dynamic.Interface, namespace string) *ArgoWorkflowAction {
	if namespace == "" {
		namespace = "argo"
	}
	return &ArgoWorkflowAction{
		client:    client,
		namespace: namespace,
	}
}

// Name returns the action name
func (a *ArgoWorkflowAction) Name() string {
	return "trigger-argo-workflow"
}

// Validate validates the action parameters
func (a *ArgoWorkflowAction) Validate(params map[string]string) error {
	if _, ok := params["workflow_template"]; !ok {
		if _, ok := params["workflow_name"]; !ok {
			return fmt.Errorf("either workflow_template or workflow_name is required")
		}
	}
	return nil
}

// Execute triggers an Argo Workflow
func (a *ArgoWorkflowAction) Execute(ctx context.Context, target Target, params map[string]string) error {
	workflowGVR := schema.GroupVersionResource{
		Group:    "argoproj.io",
		Version:  "v1alpha1",
		Resource: "workflows",
	}

	// Build workflow spec
	workflow := a.buildWorkflow(target, params)

	// Determine namespace
	namespace := a.namespace
	if ns, ok := params["namespace"]; ok && ns != "" {
		namespace = ns
	}

	// Create the workflow
	_, err := a.client.Resource(workflowGVR).Namespace(namespace).Create(
		ctx,
		workflow,
		metav1.CreateOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to create workflow: %w", err)
	}

	return nil
}

func (a *ArgoWorkflowAction) buildWorkflow(target Target, params map[string]string) *unstructured.Unstructured {
	timestamp := time.Now().Format("20060102-150405")
	workflowName := fmt.Sprintf("kube-sentinel-%s-%s", target.Pod, timestamp)

	// Truncate name if too long
	if len(workflowName) > 63 {
		workflowName = workflowName[:63]
	}

	workflow := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "argoproj.io/v1alpha1",
			"kind":       "Workflow",
			"metadata": map[string]interface{}{
				"generateName": "kube-sentinel-remediation-",
				"labels": map[string]interface{}{
					"app.kubernetes.io/managed-by": "kube-sentinel",
					"kube-sentinel/target-pod":     target.Pod,
					"kube-sentinel/target-ns":      target.Namespace,
				},
				"annotations": map[string]interface{}{
					"kube-sentinel/triggered-at": time.Now().Format(time.RFC3339),
					"kube-sentinel/target":       target.String(),
				},
			},
		},
	}

	// Check if using a WorkflowTemplate
	if templateName, ok := params["workflow_template"]; ok && templateName != "" {
		workflow.Object["spec"] = map[string]interface{}{
			"workflowTemplateRef": map[string]interface{}{
				"name": templateName,
			},
			"arguments": a.buildArguments(target, params),
		}
	} else {
		// Use inline workflow definition
		workflow.Object["spec"] = a.buildInlineSpec(target, params)
	}

	return workflow
}

func (a *ArgoWorkflowAction) buildArguments(target Target, params map[string]string) map[string]interface{} {
	arguments := map[string]interface{}{
		"parameters": []map[string]interface{}{
			{"name": "namespace", "value": target.Namespace},
			{"name": "pod", "value": target.Pod},
			{"name": "container", "value": target.Container},
		},
	}

	// Add custom parameters
	if paramsJSON, ok := params["arguments"]; ok {
		var customParams []map[string]interface{}
		if err := json.Unmarshal([]byte(paramsJSON), &customParams); err == nil {
			existingParams := arguments["parameters"].([]map[string]interface{})
			arguments["parameters"] = append(existingParams, customParams...)
		}
	}

	return arguments
}

func (a *ArgoWorkflowAction) buildInlineSpec(target Target, params map[string]string) map[string]interface{} {
	// Default remediation workflow
	spec := map[string]interface{}{
		"entrypoint": "remediate",
		"arguments": map[string]interface{}{
			"parameters": []map[string]interface{}{
				{"name": "namespace", "value": target.Namespace},
				{"name": "pod", "value": target.Pod},
				{"name": "container", "value": target.Container},
				{"name": "action", "value": params["inline_action"]},
			},
		},
		"templates": []map[string]interface{}{
			{
				"name": "remediate",
				"inputs": map[string]interface{}{
					"parameters": []map[string]interface{}{
						{"name": "namespace"},
						{"name": "pod"},
						{"name": "container"},
						{"name": "action"},
					},
				},
				"container": map[string]interface{}{
					"image":   params["image"],
					"command": []string{"/bin/sh", "-c"},
					"args":    []string{a.buildScript(params)},
					"env": []map[string]interface{}{
						{"name": "TARGET_NAMESPACE", "value": "{{inputs.parameters.namespace}}"},
						{"name": "TARGET_POD", "value": "{{inputs.parameters.pod}}"},
						{"name": "TARGET_CONTAINER", "value": "{{inputs.parameters.container}}"},
						{"name": "ACTION", "value": "{{inputs.parameters.action}}"},
					},
				},
			},
		},
		"serviceAccountName": params["service_account"],
		"ttlStrategy": map[string]interface{}{
			"secondsAfterCompletion": 3600,
			"secondsAfterSuccess":    600,
			"secondsAfterFailure":    86400,
		},
	}

	// Set default image if not provided
	if _, ok := spec["templates"].([]map[string]interface{})[0]["container"].(map[string]interface{})["image"]; !ok {
		spec["templates"].([]map[string]interface{})[0]["container"].(map[string]interface{})["image"] = "bitnami/kubectl:latest"
	}

	return spec
}

func (a *ArgoWorkflowAction) buildScript(params map[string]string) string {
	if script, ok := params["script"]; ok && script != "" {
		return script
	}

	// Default remediation script
	return `#!/bin/sh
set -e

echo "Kube Sentinel Remediation Workflow"
echo "==================================="
echo "Target: $TARGET_NAMESPACE/$TARGET_POD ($TARGET_CONTAINER)"
echo "Action: $ACTION"
echo ""

case "$ACTION" in
  restart)
    echo "Restarting pod..."
    kubectl delete pod "$TARGET_POD" -n "$TARGET_NAMESPACE" --grace-period=30
    ;;
  describe)
    echo "Describing pod..."
    kubectl describe pod "$TARGET_POD" -n "$TARGET_NAMESPACE"
    ;;
  logs)
    echo "Getting logs..."
    kubectl logs "$TARGET_POD" -n "$TARGET_NAMESPACE" -c "$TARGET_CONTAINER" --tail=100
    ;;
  events)
    echo "Getting events..."
    kubectl get events -n "$TARGET_NAMESPACE" --field-selector involvedObject.name="$TARGET_POD"
    ;;
  diagnose)
    echo "Running diagnostics..."
    kubectl describe pod "$TARGET_POD" -n "$TARGET_NAMESPACE"
    echo ""
    echo "--- Recent Events ---"
    kubectl get events -n "$TARGET_NAMESPACE" --field-selector involvedObject.name="$TARGET_POD" --sort-by='.lastTimestamp'
    echo ""
    echo "--- Container Logs ---"
    kubectl logs "$TARGET_POD" -n "$TARGET_NAMESPACE" -c "$TARGET_CONTAINER" --tail=50 || true
    ;;
  *)
    echo "Unknown action: $ACTION"
    exit 1
    ;;
esac

echo ""
echo "Remediation complete."
`
}

// WorkflowTemplateSpec represents a reusable workflow template for common remediation patterns
type WorkflowTemplateSpec struct {
	Name        string
	Description string
	Template    string
}

// GetBuiltinWorkflowTemplates returns built-in workflow templates for common remediation scenarios
func GetBuiltinWorkflowTemplates() []WorkflowTemplateSpec {
	return []WorkflowTemplateSpec{
		{
			Name:        "diagnose-pod",
			Description: "Diagnose a failing pod by collecting logs, events, and resource status",
			Template: `apiVersion: argoproj.io/v1alpha1
kind: WorkflowTemplate
metadata:
  name: diagnose-pod
  labels:
    app.kubernetes.io/managed-by: kube-sentinel
spec:
  entrypoint: diagnose
  arguments:
    parameters:
      - name: namespace
      - name: pod
      - name: container
  templates:
    - name: diagnose
      inputs:
        parameters:
          - name: namespace
          - name: pod
          - name: container
      dag:
        tasks:
          - name: describe
            template: kubectl-cmd
            arguments:
              parameters:
                - name: cmd
                  value: "describe pod {{inputs.parameters.pod}} -n {{inputs.parameters.namespace}}"
          - name: logs
            template: kubectl-cmd
            arguments:
              parameters:
                - name: cmd
                  value: "logs {{inputs.parameters.pod}} -n {{inputs.parameters.namespace}} -c {{inputs.parameters.container}} --tail=200"
          - name: events
            template: kubectl-cmd
            arguments:
              parameters:
                - name: cmd
                  value: "get events -n {{inputs.parameters.namespace}} --field-selector involvedObject.name={{inputs.parameters.pod}} --sort-by='.lastTimestamp'"
    - name: kubectl-cmd
      inputs:
        parameters:
          - name: cmd
      container:
        image: bitnami/kubectl:latest
        command: ["/bin/sh", "-c"]
        args: ["kubectl {{inputs.parameters.cmd}}"]
`,
		},
		{
			Name:        "restart-with-backup",
			Description: "Restart a pod after backing up its logs",
			Template: `apiVersion: argoproj.io/v1alpha1
kind: WorkflowTemplate
metadata:
  name: restart-with-backup
  labels:
    app.kubernetes.io/managed-by: kube-sentinel
spec:
  entrypoint: restart-with-backup
  arguments:
    parameters:
      - name: namespace
      - name: pod
      - name: container
  templates:
    - name: restart-with-backup
      inputs:
        parameters:
          - name: namespace
          - name: pod
          - name: container
      steps:
        - - name: backup-logs
            template: backup-logs
            arguments:
              parameters:
                - name: namespace
                  value: "{{inputs.parameters.namespace}}"
                - name: pod
                  value: "{{inputs.parameters.pod}}"
                - name: container
                  value: "{{inputs.parameters.container}}"
        - - name: restart-pod
            template: restart-pod
            arguments:
              parameters:
                - name: namespace
                  value: "{{inputs.parameters.namespace}}"
                - name: pod
                  value: "{{inputs.parameters.pod}}"
    - name: backup-logs
      inputs:
        parameters:
          - name: namespace
          - name: pod
          - name: container
      container:
        image: bitnami/kubectl:latest
        command: ["/bin/sh", "-c"]
        args:
          - |
            echo "Backing up logs for {{inputs.parameters.pod}}..."
            kubectl logs {{inputs.parameters.pod}} -n {{inputs.parameters.namespace}} -c {{inputs.parameters.container}} --tail=1000 > /tmp/pod-logs.txt
            echo "Logs backed up successfully"
      outputs:
        artifacts:
          - name: pod-logs
            path: /tmp/pod-logs.txt
    - name: restart-pod
      inputs:
        parameters:
          - name: namespace
          - name: pod
      container:
        image: bitnami/kubectl:latest
        command: ["/bin/sh", "-c"]
        args:
          - |
            echo "Restarting pod {{inputs.parameters.pod}}..."
            kubectl delete pod {{inputs.parameters.pod}} -n {{inputs.parameters.namespace}} --grace-period=30
            echo "Pod restart initiated"
`,
		},
		{
			Name:        "scale-and-monitor",
			Description: "Scale a deployment and monitor for health",
			Template: `apiVersion: argoproj.io/v1alpha1
kind: WorkflowTemplate
metadata:
  name: scale-and-monitor
  labels:
    app.kubernetes.io/managed-by: kube-sentinel
spec:
  entrypoint: scale-and-monitor
  arguments:
    parameters:
      - name: namespace
      - name: deployment
      - name: replicas
        value: "3"
  templates:
    - name: scale-and-monitor
      inputs:
        parameters:
          - name: namespace
          - name: deployment
          - name: replicas
      steps:
        - - name: scale
            template: scale-deployment
            arguments:
              parameters:
                - name: namespace
                  value: "{{inputs.parameters.namespace}}"
                - name: deployment
                  value: "{{inputs.parameters.deployment}}"
                - name: replicas
                  value: "{{inputs.parameters.replicas}}"
        - - name: wait-ready
            template: wait-for-rollout
            arguments:
              parameters:
                - name: namespace
                  value: "{{inputs.parameters.namespace}}"
                - name: deployment
                  value: "{{inputs.parameters.deployment}}"
    - name: scale-deployment
      inputs:
        parameters:
          - name: namespace
          - name: deployment
          - name: replicas
      container:
        image: bitnami/kubectl:latest
        command: ["/bin/sh", "-c"]
        args:
          - kubectl scale deployment {{inputs.parameters.deployment}} -n {{inputs.parameters.namespace}} --replicas={{inputs.parameters.replicas}}
    - name: wait-for-rollout
      inputs:
        parameters:
          - name: namespace
          - name: deployment
      container:
        image: bitnami/kubectl:latest
        command: ["/bin/sh", "-c"]
        args:
          - kubectl rollout status deployment {{inputs.parameters.deployment}} -n {{inputs.parameters.namespace}} --timeout=300s
`,
		},
	}
}

// RenderWorkflowTemplate renders a workflow template with the given parameters
func RenderWorkflowTemplate(tmpl string, params map[string]string) (string, error) {
	t, err := template.New("workflow").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, params); err != nil {
		return "", fmt.Errorf("failed to render template: %w", err)
	}

	return buf.String(), nil
}
