# Copyright 2017 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -x -ldflags="-extldflags=-static" -o ./bin/ ./cmd/nfsexportplugin

clean: 
	rm -vfr ./bin/nfsexportplugin

run-local-provisioner: 
	./bin/csi-provisioner --kubeconfig ~/.kube/config -v 5 --csi-address /usr/local/var/run/csi/socket

run-local-plugin:
	go run ./cmd/nfsexportplugin/ -v 5 --endpoint unix:///usr/local/var/run/csi/socket

run-local-nfs-server:
	docker rm -f nfs-ganesha
	docker run --name nfs-ganesha \
		--privileged \
		-d --restart=unless-stopped \
		-v /Users/alexz/nfs:/export \
		daocloud.io/piraeus/volume-nfs-exporter:ganesha
	docker ps | grep nfs-ganesha

rn: clean build
	kubectl delete -f run/csi-nfs-node.yaml || true
	while kubectl get pod | grep csi-nfs-node; do sleep 1; done
	kubectl apply -f run/csi-nfs-node.yaml
	watch kubectl get pod

rp: clean build
	kubectl delete -f run/csi-nfs-controller.yaml || true
	while kubectl get pod | grep csi-nfs-controller; do sleep 1; done
	kubectl apply -f run/csi-nfs-controller.yaml
	watch kubectl get pod

test:
	kubectl apply -f example/pvc-dynamic.yaml

untest:
	kubectl delete -f example/pvc-dynamic.yaml
