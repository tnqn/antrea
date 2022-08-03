#!/bin/bash

# Create apps
kubectl apply -f apps.yaml

# Initially, all access are allowed
kubectl exec deploy/client -n dev1 -- curl -s nginx.dev1
kubectl exec deploy/client -n dev1 -- curl -s nginx.dev2
kubectl exec deploy/client -n dev2 -- curl -s nginx.dev1
kubectl exec deploy/client -n dev2 -- curl -s nginx.dev2

# Apply baseline policy, all access are denied except DNS service
kubectl apply -f deny-all-except-dns.yaml

kubectl exec deploy/client -n dev1 -- curl -s nginx.dev1 --connect-timeout 1
kubectl exec deploy/client -n dev1 -- curl -s nginx.dev2 --connect-timeout 1
kubectl exec deploy/client -n dev2 -- curl -s nginx.dev1 --connect-timeout 1
kubectl exec deploy/client -n dev2 -- curl -s nginx.dev2 --connect-timeout 1

# ACNP status, stats, logging
kubectl get acnp

kubectl get antreaclusternetworkpolicystats

kk exec -it antrea-agent-6h942 -- tail -f /var/log/antrea/networkpolicy/np.log

# Apply self namespace policy, all intra-namespace access are allowed
kubectl apply -f allow-intra-namespace.yaml

kubectl exec deploy/client -n dev1 -- curl -s nginx.dev1 --connect-timeout 1
kubectl exec deploy/client -n dev1 -- curl -s nginx.dev2 --connect-timeout 1
kubectl exec deploy/client -n dev2 -- curl -s nginx.dev1 --connect-timeout 1
kubectl exec deploy/client -n dev2 -- curl -s nginx.dev2 --connect-timeout 1

kubectl get acnp

kubectl get antreaclusternetworkpolicystats

# Accessing FQDN is allowed
kubectl exec deploy/client -n dev1 -- curl -s https://api.github.com/zen --connect-timeout 1

# Apply FQDN denying policy, accessing FQDN is denied
kubectl apply -f deny-fqdn.yaml

kubectl exec deploy/client -n dev1 -- curl -s https://api.github.com/zen --connect-timeout 1

# ACNP status, stats, logging
kubectl get acnp

kubectl get antreaclusternetworkpolicystats

kk exec -it antrea-agent-6h942 -- tail -f /var/log/antrea/networkpolicy/np.log


# IDS

# Apply TC Mirror, we can see mirrored traffic on tap0
kubectl apply -f tc-mirror.yaml

# New terminal
nsenter -n -t `docker inspect kind-worker -f '{{ .State.Pid }}'`
tcpdump -i tap0 -n

# Deploy Suricata service, watch its alerts
kubectl apply -f https://raw.githubusercontent.com/antrea-io/antrea/main/docs/cookbooks/ids/resources/suricata.yml

# New terminal
docker exec kind-worker tail -f /var/log/suricata/fast.log

# Simulate malicious attack
kubectl exec deploy/client -n dev1 -- curl -s 10.244.1.2/dlink/hwiz.html
kubectl exec nginx-5c47646cdb-jx4r2 -- curl -s http://testmynids.org/uid/index.html
