package main

import (
	"context"
	"fmt"

	v1 "github.com/rancher/wrangler-api/pkg/generated/controllers/apps/v1"
	samplev1alpha1 "github.com/rancher/wrangler-sample/pkg/apis/samplecontroller.k8s.io/v1alpha1"
	samplescheme "github.com/rancher/wrangler-sample/pkg/generated/clientset/versioned/scheme"
	"github.com/rancher/wrangler-sample/pkg/generated/controllers/samplecontroller.k8s.io/v1alpha1"
	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
)

const controllerAgentName = "sample-controller"

const (
	// ErrResourceExists is used as part of the Event 'reason' when a Foo fails
	// to sync due to a Deployment of the same name already existing.
	ErrResourceExists = "ErrResourceExists"

	// MessageResourceExists is the message used for Events when a resource
	// fails to sync due to a Deployment already existing
	MessageResourceExists = "Resource %q already exists and is not managed by Foo"
)

// Handler is the controller implementation for Foo resources
type Handler struct {
	deployments      v1.DeploymentClient
	deploymentsCache v1.DeploymentCache
	foos             v1alpha1.FooController
	foosCache        v1alpha1.FooCache
	recorder         record.EventRecorder
}

// NewController returns a new sample controller
func Register(
	ctx context.Context,
	events typedcorev1.EventInterface,
	deployments v1.DeploymentController,
	foos v1alpha1.FooController) {

	controller := &Handler{
		deployments:      deployments,
		deploymentsCache: deployments.Cache(),
		foos:             foos,
		foosCache:        foos.Cache(),
		recorder:         buildEventRecorder(events),
	}

	// Register handlers
	deployments.OnChange(ctx, "foo-handler", controller.OnDeploymentChanged)
	foos.OnChange(ctx, "foo-handler", controller.OnFooChanged)
}

func buildEventRecorder(events typedcorev1.EventInterface) record.EventRecorder {
	// Create event broadcaster
	// Add sample-controller types to the default Kubernetes Scheme so Events can be
	// logged for sample-controller types.
	utilruntime.Must(samplescheme.AddToScheme(scheme.Scheme))
	logrus.Info("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(logrus.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: events})
	return eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})
}

func (h *Handler) OnFooChanged(key string, foo *samplev1alpha1.Foo) (*samplev1alpha1.Foo, error) {
	// foo will be nil if key is deleted from cache
	if foo == nil {
		return nil, nil
	}

	deploymentName := foo.Spec.DeploymentName
	if deploymentName == "" {
		// We choose to absorb the error here as the worker would requeue the
		// resource otherwise. Instead, the next time the resource is updated
		// the resource will be queued again.
		utilruntime.HandleError(fmt.Errorf("%s: deployment name must be specified", key))
		return nil, nil
	}

	// Get the deployment with the name specified in Foo.spec
	deployment, err := h.deploymentsCache.Get(foo.Namespace, deploymentName)
	// If the resource doesn't exist, we'll create it
	if errors.IsNotFound(err) {
		deployment, err = h.deployments.Create(newDeployment(foo))
	}

	// If an error occurs during Get/Create, we'll requeue the item so we can
	// attempt processing again later. This could have been caused by a
	// temporary network failure, or any other transient reason.
	if err != nil {
		return nil, err
	}

	// If the Deployment is not controlled by this Foo resource, we should log
	// a warning to the event recorder and ret
	if !metav1.IsControlledBy(deployment, foo) {
		msg := fmt.Sprintf(MessageResourceExists, deployment.Name)
		h.recorder.Event(foo, corev1.EventTypeWarning, ErrResourceExists, msg)
		// Notice we don't return an error here, this is intentional because an
		// error means we should retry to reconcile.  In this situation we've done all
		// we could, which was log an error.
		return nil, nil
	}

	// If this number of the replicas on the Foo resource is specified, and the
	// number does not equal the current desired replicas on the Deployment, we
	// should update the Deployment resource.
	if foo.Spec.Replicas != nil && *foo.Spec.Replicas != *deployment.Spec.Replicas {
		logrus.Infof("Foo %s replicas: %d, deployment replicas: %d", foo.Name, *foo.Spec.Replicas, *deployment.Spec.Replicas)
		deployment, err = h.deployments.Update(newDeployment(foo))
	}

	// If an error occurs during Update, we'll requeue the item so we can
	// attempt processing again later. THis could have been caused by a
	// temporary network failure, or any other transient reason.
	if err != nil {
		return nil, err
	}

	// Finally, we update the status block of the Foo resource to reflect the
	// current state of the world
	err = h.updateFooStatus(foo, deployment)
	if err != nil {
		return nil, err
	}

	return nil, nil
}

func (h *Handler) updateFooStatus(foo *samplev1alpha1.Foo, deployment *appsv1.Deployment) error {
	// NEVER modify objects from the store. It's a read-only, local cache.
	// You can use DeepCopy() to make a deep copy of original object and modify this copy
	// Or create a copy manually for better performance
	fooCopy := foo.DeepCopy()
	fooCopy.Status.AvailableReplicas = deployment.Status.AvailableReplicas
	// If the CustomResourceSubresources feature gate is not enabled,
	// we must use Update instead of UpdateStatus to update the Status block of the Foo resource.
	// UpdateStatus will not allow changes to the Spec of the resource,
	// which is ideal for ensuring nothing other than resource status has been updated.
	_, err := h.foos.Update(fooCopy)
	return err
}

func (h *Handler) OnDeploymentChanged(key string, deployment *appsv1.Deployment) (*appsv1.Deployment, error) {
	// When an item is deleted the deployment is nil, just ignore
	if deployment == nil {
		return nil, nil
	}

	if ownerRef := metav1.GetControllerOf(deployment); ownerRef != nil {
		// If this object is not owned by a Foo, we should not do anything more
		// with it.
		if ownerRef.Kind != "Foo" {
			return nil, nil
		}

		foo, err := h.foosCache.Get(deployment.Namespace, ownerRef.Name)
		if err != nil {
			logrus.Infof("ignoring orphaned object '%s' of foo '%s'", deployment.GetSelfLink(), ownerRef.Name)
			return nil, nil
		}

		h.foos.Enqueue(foo.Namespace, foo.Name)
		return nil, nil
	}

	return nil, nil
}

// newDeployment creates a new Deployment for a Foo resource. It also sets
// the appropriate OwnerReferences on the resource so handleObject can discover
// the Foo resource that 'owns' it.
func newDeployment(foo *samplev1alpha1.Foo) *appsv1.Deployment {
	labels := map[string]string{
		"app":        "nginx",
		"controller": foo.Name,
	}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      foo.Spec.DeploymentName,
			Namespace: foo.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(foo, schema.GroupVersionKind{
					Group:   samplev1alpha1.SchemeGroupVersion.Group,
					Version: samplev1alpha1.SchemeGroupVersion.Version,
					Kind:    "Foo",
				}),
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: foo.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx:latest",
						},
					},
				},
			},
		},
	}
}
