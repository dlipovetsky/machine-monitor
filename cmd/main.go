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

package main

import (
	"flag"
	"log"
	"os"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/dlipovetsky/machine-monitor/internal/controller"
	"github.com/go-logr/stdr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	cabpkv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1beta1"
	// +kubebuilder:scaffold:imports
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(clusterv1.AddToScheme(scheme))
	utilruntime.Must(cabpkv1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

type Config struct {
	SSHPort               int
	SSHUser               string
	SSHPrivateKeyFileName string

	BastionSSHHost               string
	BastionSSHPort               int
	BastionSSHUser               string
	BastionSSHPrivateKeyFileName string

	LocalJournalDirectory        string
	RemoteJournaldCursorFilePath string

	MaxConcurrentReconciles int
	RequeueBaseDelay        time.Duration
	RequeueMaxDelay         time.Duration
	LabelSelectors          string
}

// nolint:gocyclo
func main() {
	config := Config{}

	flag.IntVar(
		&config.SSHPort,
		"ssh-port",
		22,
		"The port for the SSH connection to the machines.",
	)
	flag.StringVar(
		&config.SSHUser,
		"ssh-user",
		"",
		"The username for the SSH connection to the machines. The user must have sudo privileges.",
	)
	flag.StringVar(
		&config.SSHPrivateKeyFileName,
		"ssh-private-key",
		"",
		"The path to the private key file for the SSH connection to the machines.")

	flag.StringVar(
		&config.BastionSSHHost,
		"bastion-ssh-host",
		"",
		"The host of the bastion server. If not provided, no bastion server will be used.",
	)
	flag.IntVar(
		&config.BastionSSHPort,
		"bastion-ssh-port",
		22,
		"The port of the bastion server.",
	)
	flag.StringVar(
		&config.BastionSSHUser,
		"bastion-ssh-user",
		"",
		"The username for the SSH connection to the bastion server.",
	)
	flag.StringVar(
		&config.BastionSSHPrivateKeyFileName,
		"bastion-ssh-private-key",
		"",
		"The path to the private key file for the SSH connection to the bastion server.",
	)

	flag.StringVar(
		&config.LocalJournalDirectory,
		"local-journal-directory",
		"",
		"The directory to store the local journal files. Default is the current working directory.")
	flag.StringVar(
		&config.RemoteJournaldCursorFilePath,
		"remote-journald-cursor-file-path",
		"$HOME/machine-monitor-journald.cursor",
		"The path used to store the journald cursor file on the remote machine.",
	)

	flag.StringVar(
		&config.LabelSelectors,
		"label-selectors",
		"",
		"The label selectors to filter the machines to monitor. Empty string means all machines.")
	flag.IntVar(
		&config.MaxConcurrentReconciles,
		"max-concurrent-reconciles",
		10,
		"The maximum number of concurrent reconciles to run.",
	)
	flag.DurationVar(
		&config.RequeueBaseDelay,
		"requeue-base-delay",
		time.Second*10,
		"The base delay for requeuing a machine after an error.",
	)
	flag.DurationVar(
		&config.RequeueMaxDelay,
		"requeue-max-delay",
		time.Minute*2,
		"The max delay for requeuing a machine after an error.",
	)

	var logLevel int
	flag.IntVar(&logLevel,
		"log-level",
		0,
		"The log verbosity.",
	)

	// All flags must be defined before Parse() is called.
	flag.Parse()

	stdr.SetVerbosity(logLevel)
	logger := stdr.New(log.New(os.Stderr, "", 0))

	var labelSelector *metav1.LabelSelector
	if config.LabelSelectors != "" {
		var err error
		labelSelector, err = metav1.ParseToLabelSelector(config.LabelSelectors)
		if err != nil {
			logger.Error(err, "unable to parse label selector")
			defer os.Exit(1)
			return
		}
	}

	sshPrivateKey, err := os.ReadFile(config.SSHPrivateKeyFileName)
	if err != nil {
		logger.Error(err, "unable to read SSH private key file")
		defer os.Exit(1)
		return
	}

	var bastionSSHPrivateKey []byte
	if config.BastionSSHHost != "" {
		bastionSSHPrivateKey, err = os.ReadFile(config.BastionSSHPrivateKeyFileName)
		if err != nil {
			logger.Error(err, "unable to read bastion SSH private key file")
			defer os.Exit(1)
			return
		}
	}

	ctrl.SetLogger(logger)
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
	})
	if err != nil {
		logger.Error(err, "unable to start manager")
		defer os.Exit(1)
		return
	}

	if err := (&controller.MachineReconciler{
		Client: mgr.GetClient(),

		SSHPrivateKey: sshPrivateKey,
		SSHUser:       config.SSHUser,
		SSHPort:       config.SSHPort,

		BastionSSHPrivateKey: bastionSSHPrivateKey,
		BastionSSHUser:       config.BastionSSHUser,
		BastionSSHPort:       config.BastionSSHPort,
		BastionSSHHost:       config.BastionSSHHost,

		LocalJournalDirectory:        config.LocalJournalDirectory,
		RemoteJournaldCursorFilePath: config.RemoteJournaldCursorFilePath,

		MaxConcurrentReconciles: config.MaxConcurrentReconciles,
		RequeueBaseDelay:        config.RequeueBaseDelay,
		RequeueMaxDelay:         config.RequeueMaxDelay,
		LabelSelector:           labelSelector,
	}).SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to create controller", "controller", "Machine")
		defer os.Exit(1)
		return
	}
	// +kubebuilder:scaffold:builder

	logger.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error(err, "problem running manager")
		defer os.Exit(1)
		return
	}
}
