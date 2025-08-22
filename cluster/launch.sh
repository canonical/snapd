set -e

hosts="$(seq 2 8 | xargs -L1 printf 'host-%d\n')"
for host in ${hosts}; do
    multipass exec "${host}" sudo -- snap abort --last=assemble-cluster &> /dev/null || true
    multipass exec "${host}" sudo -- snap wait --last=assemble-cluster &> /dev/null || true
done

for host in ${hosts}; do
    echo "starting assembly on host ${host}..."
    addr=$(multipass exec "${host}" bash -- -c "ip -4 -o addr show dev ens3 | awk '{print \$4}' | cut -d/ -f1")
    multipass exec "${host}" sudo -- snap cluster assemble --secret=secret --address="${addr}:8080" --domain=multipass --no-wait &> /dev/null
done

host="host-1"
echo "starting assembly on host ${host}..."
multipass exec "${host}" sudo -- snap abort --last=assemble-cluster &> /dev/null || true
multipass exec "${host}" sudo -- snap wait --last=assemble-cluster &> /dev/null || true
addr=$(multipass exec "${host}" bash -- -c "ip -4 -o addr show dev ens3 | awk '{print \$4}' | cut -d/ -f1")
echo snap cluster assemble --secret=secret --address="${addr}:8080" --domain=multipass --expected-size=20
multipass exec "${host}" sudo -- snap cluster assemble --secret=secret --address="${addr}:8080" --domain=multipass --expected-size=20
