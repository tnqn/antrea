#!/bin/bash

# Installation
helm install antrea antrea/antrea -n kube-system

export NGINX=projects.registry.vmware.com/antrea/nginx:1.21.6-alpine
export AGNHOST=k8s.gcr.io/e2e-test-images/agnhost:2.29

kubectl create deploy test --image=$NGINX --replicas=3

# 01-anp.yaml
helm uninstall antrea -n kube-system
helm install antrea antrea/antrea --namespace kube-system --set featureGates.L7NetworkPolicy=true,disableTXChecksumOffload=true

kubectl create deploy client --image=$AGNHOST
kubectl create deploy server --image=$NGINX

kubectl exec -it deploy/client -- curl x.x.x.x

kubectl apply -f 01-anp.yaml

kubectl get anp
kubectl get antreanetworkpolicystats
kubectl exec -n kube-system -it antrea-agent-lpm2m -- cat /var/log/antrea/networkpolicy/np.log

# 02-zero-trust

# 03-strict-ns-isolation

kubectl create ns other
kubectl create deploy client --image=$AGNHOST -n other
kubectl create deploy server --image=$NGINX -n other
kubectl create deploy test --image=$AGNHOST -n other

# 04-to-services

# Create apps
kubectl apply -f apps.yaml
