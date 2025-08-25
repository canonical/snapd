set -ex

hosts="$(seq 8 | xargs -L1 printf 'host-%d\n')"

for host in ${hosts}; do
    (
        multipass transfer ./snapd_2.71_amd64.snap "${host}:"
        multipass exec "${host}" sudo -- snap install --dangerous ./snapd_2.71_amd64.snap

        # import assertions needed to trust the cluster assertion
        multipass exec "${host}" bash -- -c 'snap known --remote account-key public-key-sha3-384=BWDEoaqyr25nF5SNCvEv2v7QnM9QsfCc0PBMYD_i2NGSQ32EF2d4D0hqUel3m8ul | sudo snap ack /dev/stdin'
        multipass exec "${host}" bash -- -c 'snap known --remote account account-id=ANwAAfhfp2IwsUkNeI3R1xqCYE1kXeIm | sudo snap ack /dev/stdin'
    ) &
done

wait
