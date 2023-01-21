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

REG = daocloud.io/piraeus

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	go build -x -ldflags="-extldflags=-static" -o ./bin/ ./cmd/nfsexportplugin

clean:
	rm -vfr ./bin/nfsexportplugin

docker: clean build
	docker build -t	$(REG)/csi-nfs-export-plugin .
	docker push		$(REG)/csi-nfs-export-plugin || \
	docker push		$(REG)/csi-nfs-export-plugin

nfs-ganesha:
	cd exporter/nfs-ganesha && \
	docker build -t $(REG)/nfs-ganesha .
	docker push		$(REG)/nfs-ganesha || \
	docker push		$(REG)/nfs-ganesha

nfs-server:
	cd exporter/nfs-server && \
	docker build -t $(REG)/nfs-server .
	docker push		$(REG)/nfs-server || \
	docker push		$(REG)/nfs-server
	
.PHONY: deploy
deploy:
	kubectl apply -f deploy
	kubectl rollout status deploy,ds --timeout=90s
	kubectl get pod -l nfs-export.csi.k8s.io/server

undeploy: 
	kubectl delete -f deploy/ || true
	kubectl wait po -l nfs-export.csi.k8s.io/server --for=delete --timeout=90s
	kubectl get pod -l nfs-export.csi.k8s.io/server

.PHONY: test
test:
	kubectl apply -f example/storageclass.yaml
	kubectl apply -f example/pvc-dynamic.yaml
	kubectl apply -f example/deployment-dynamic.yaml
	watch kubectl get pod -o wide -l nfs-export.csi.k8s.io/id

untest:
	kubectl delete -f example/deployment-dynamic.yaml || true
	kubectl wait pod -l nfs-export.csi.k8s.io/id=deployment-nginx-dynamic --for=delete --timeout=90s
	kubectl delete -f example/pvc-dynamic.yaml || true
	kubectl delete sts,svc,pvc -l nfs-export.csi.k8s.io/id
	kubectl delete -f example/storageclass.yaml || true
	watch kubectl get pod -o wide -l nfs-export.csi.k8s.io/id

# Controller Log
logc: 
	kubectl logs -f deploy/csi-nfs-export-controller nfs-export

# Local node log
logn:
	node=$$(kubectl get pod -o wide -l nfs-export.csi.k8s.io/id | awk '/^nfs-/{print $$7}' | head -1); \
	pod=$$(kubectl get pod -o wide -l nfs-export.csi.k8s.io/server=node --field-selector spec.nodeName=$$node -o name); \
	kubectl logs -f $$pod nfs-export

start-local-nfs-server:
	docker rm -f nfs-ganesha
	docker run --name nfs-ganesha \
		--privileged \
		-d --restart=unless-stopped \
		-v nfs-ganesha:/export \
		$(REG)/nfs-ganesha
	docker ps | grep nfs-ganesha
