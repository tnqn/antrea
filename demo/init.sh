#!/bin/bash

cd ~/antrea

#docker pull busybox:1.27.2
#docker pull projects.registry.vmware.com/antrea/nginx
docker pull qtian/toolbox:latest
docker pull networkstatic/iperf3:latest
docker pull jasonish/suricata:latest

kind create cluster --config kind.yml
kind load docker-image qtian/toolbox:latest networkstatic/iperf3:latest jasonish/suricata:latest

# Installation
#kubectl apply -f https://github.com/antrea-io/antrea/releases/download/v1.7.1/antrea.yml

curl -sL https://github.com/antrea-io/antrea/releases/download/v1.7.1/antrea.yml | \
  sed "s/.*TrafficControl:.*/      TrafficControl: true/" | \
  sed "s/.*FlowExporter:.*/      FlowExporter: true/" | \
  kubectl apply -f -


cd ~/theia
git checkout release-0.1

helm install theia build/charts/theia --set sparkOperator.enable=true -n flow-visibility --create-namespace
helm install flow-aggregator build/charts/flow-aggregator --set clickHouse.enable=true,recordContents.podLabels=true -n flow-aggregator --create-namespace

kubectl port-forward svc/grafana -n flow-visibility --address 0.0.0.0 8080:3000

http://10.176.3.138:8080







# Octant
kubectl create secret generic octant-kubeconfig --from-file=admin.conf=/etc/kubernetes/admin.conf -n kube-system
kubectl apply -f https://github.com/antrea-io/antrea/releases/download/v1.4.0/antrea-octant.yml

# Egress
docker run -p 0.0.0.0:8080:8080 --rm k8s.gcr.io/e2e-test-images/agnhost:2.29 netexec

# Visibility
#kubectl apply -f https://github.com/vmware/go-ipfix/releases/download/v0.5.10/ipfix-collector.yaml

cd ~/antrea/build/yamls/
kubectl create namespace elk-flow-collector
kubectl create configmap logstash-configmap -n elk-flow-collector --from-file=./elk-flow-collector/logstash/
kubectl apply -f ./elk-flow-collector/elk-flow-collector.yml -n elk-flow-collector

kubectl apply -f https://github.com/antrea-io/antrea/releases/download/v1.4.0/flow-aggregator.yml


kubectl exec deploy/client -n dev1 -- bash -c "while true; do curl -4 nginx ; sleep 1; done "

