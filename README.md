# machine-monitor

 Machine-monitor fetches the journald default namespace journal from every Machine created by Cluster API. It is intended to improve troubleshooting of the bootstrap and shutdown process of these Machines, i.e., when the Machine does not have a corresponding Node resource, and its journal be inspected via other means, like a debug Pod.

 Machine-monitor requires SSH access to Machines. This means that the bootstrap process must configure networking and SSH before machine-monitor can fetch the journal.

## Install

### Prerequisites

- go version v1.24.6+
- make

### Build

```shell
make build
```

## Use

Machine-monitor collects journald journals continuously until it is terminated. The journals are stored in a local directory.

### Prerequisites

-  Local directory to store journals
-  SSH private key
-  Machines configured with an SSH user
    - who has the ability to elevate privileges using `sudo`
    - whose authorized key corresponds to SSH private key
-  Network access to SSH port of Machines
-  Access to Kubernetes API with Cluster API Machine resources

### Examples

#### Default

- Monitors up to 10 machines

```shell
mm \
-ssh-user=user \
-ssh-private-key=private_key_file \
-local-journal-directory=/tmp/machine-monitor
```

#### Advanced

- Monitors up to 50 machines
- Only monitors machines of Cluster API clusters named "example"
- Stores remote journald cursor file in a custom directory

```shell
mm \
-ssh-user=user \
-ssh-private-key=private_key_file \
-local-journal-directory=/tmp/machine-monitor \
-label-selectors=cluster.x-k8s.io/cluster-name==example \
-max-concurrent-reconciles=50 \
-remote-journald-cursor-file-path=/var/tmp/mm-journald-cursor
```

## License

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

