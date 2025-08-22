set -ex

hosts="$(seq 8 | xargs -L1 printf 'host-%d\n')"
for host in ${hosts}; do
    multipass launch -m 1G --name "${host}" noble
    multipass exec "${host}" bash -- -c "echo SNAPD_DEBUG=1 | sudo tee -a /etc/environment"
    multipass exec "${host}" sudo -- systemctl daemon-reload
    multipass exec "${host}" sudo -- systemctl restart snapd
done

for host in ${hosts}; do
    (
        multipass transfer ./snapd_2.71_amd64.snap "${host}:"
        multipass exec "${host}" sudo -- snap install --dangerous ./snapd_2.71_amd64.snap
    ) &
done

wait
