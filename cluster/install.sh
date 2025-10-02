set -e

hosts="$(seq 4 | xargs -L1 printf 'demo-%d\n')"

for host in ${hosts}; do
    (
        multipass transfer ./snapd_2.71_amd64.snap "${host}:"
        multipass exec "${host}" sudo -- snap install --dangerous ./snapd_2.71_amd64.snap
    ) &
done

wait
