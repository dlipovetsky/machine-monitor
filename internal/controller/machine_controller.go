/*
Copyright 2025 Daniel Lipovetsky.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"path"
	"time"

	"github.com/dlipovetsky/machine-monitor/internal/journald"
	"github.com/dlipovetsky/machine-monitor/internal/ssh"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// MachineReconciler reconciles a Machine object
type MachineReconciler struct {
	Client client.Client

	SSHPrivateKey []byte
	SSHUser       string
	SSHPort       int
	LabelSelector *metav1.LabelSelector

	LocalJournalDirectory        string
	RemoteJournaldCursorFilePath string

	MaxConcurrentReconciles int
	RequeueBaseDelay        time.Duration
	RequeueMaxDelay         time.Duration

	controller controller.Controller
}

// +kubebuilder:rbac:groups=machine.cluster.x-k8s.io,resources=machines,verbs=get;list;watch
// +kubebuilder:rbac:groups=machine.cluster.x-k8s.io,resources=machines/status,verbs=get

// Reconcile the Machine resource.
// If the Machine has an IP address, it will stream its journal to a local file, making sure that
// the entire journal is streamed, and that entries already in the local file are not streamed again
func (r *MachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if cause := context.Cause(ctx); cause != nil {
		// A worker may be in the queue, but not yet running, when the context is cancelled.
		// To allow the process to exit faster, we exit early.
		return ctrl.Result{}, cause
	}

	log := logf.FromContext(ctx)

	machine := &clusterv1.Machine{}
	err := r.Client.Get(ctx, req.NamespacedName, machine)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get machine %s: %w", req.NamespacedName, err)
	}
	if apierrors.IsNotFound(err) {
		// Machine was deleted after we received the request, so there is nothing to do.
		return ctrl.Result{}, nil
	}

	// Get the machine IP from the status.
	machineIP := ""
	for _, addr := range machine.Status.Addresses {
		if addr.Type == clusterv1.MachineInternalIP {
			machineIP = addr.Address
			break
		}
	}
	if machineIP == "" {
		// We expect to be requeued by the Machine status update event, so we do not explicitly requeue.
		return ctrl.Result{}, nil
	}

	log.V(1).Info("Machine IP found",
		"name",
		machine.Name,
		"ip",
		machineIP,
	)

	sshClient, err := ssh.NewClient(ctx, machineIP, r.SSHPort, r.SSHUser, r.SSHPrivateKey)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create SSH client: %w", err)
	}
	defer func() {
		if err := sshClient.Close(); err != nil {
			log.Error(err, "failed to close SSH client")
		}
	}()

	localJournalFilePath := path.Join(
		r.LocalJournalDirectory,
		// The machine name is unique in a namespace, so we use both the namespace
		// and the name to ensure the local journal file name is unique.
		fmt.Sprintf(
			"%s-%s.log",
			machine.Namespace,
			machine.Name,
		),
	)

	err = journald.StreamFromRemote(
		ctx,
		sshClient,
		r.RemoteJournaldCursorFilePath,
		localJournalFilePath,
	)
	if err != nil {
		// If we have an unexpected error, we return an error, and the controller will requeue the machine.
		// We rely on the retry-backoff mechanism to avoid overwhelming the remote machine.
		return ctrl.Result{}, fmt.Errorf("failed to import journal from remote: %w", err)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *MachineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	b := ctrl.NewControllerManagedBy(mgr).Named("machinemonitor")

	var forOpts []builder.ForOption
	if r.LabelSelector != nil {
		labelSelectorPredicate, err := predicate.LabelSelectorPredicate(*r.LabelSelector)
		if err != nil {
			return fmt.Errorf("failed to create label selector predicate: %w", err)
		}
		forOpts = append(forOpts, builder.WithPredicates(labelSelectorPredicate))
	}
	b = b.For(&clusterv1.Machine{}, forOpts...)

	b = b.WithOptions(controller.Options{
		MaxConcurrentReconciles: r.MaxConcurrentReconciles,
		RateLimiter: workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](
			r.RequeueBaseDelay,
			r.RequeueMaxDelay),
	})

	c, err := b.Build(r)
	if err != nil {
		return fmt.Errorf("failed setting up with a controller manager: %w", err)
	}

	r.controller = c
	return nil
}
