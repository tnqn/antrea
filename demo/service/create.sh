for i in `seq 1 1000`; do kubectl expose deploy/nginx --port=$i --target-port=$i --name=service$i; done


for i in `seq 2 1000`; do kubectl delete service service$i; done