NSDIR=/var/run/netns

link_netns () {
    local container=$1

    # ensure network namespace directory exists
    if [ ! -d ${NSDIR} ]; then
        mkdir -p ${NSDIR}
    fi

    # ensure container is linked in network namespace directory
    if [ ! -e ${NSDIR}/${container} ]; then
        # grab container network namespace from docker
        docker=$(command -v docker.io || command -v docker)
        nspid=$(${docker} inspect -f '{{ .State.Pid }}' ${container})

        # set up symlink to container
        cd ${NSDIR}
        rm -f ${container}
        ln -s /proc/${nspid}/ns/net ${container}
    fi

    # now "ip netns exec ${container} ..." commands will work as expected
}

create_bridge () {
    local bname=$1 stp=$2

    # create bridge with specified STP paramters, then bring it up
    brctl addbr ${bname} || true
    brctl stp ${bname} ${stp}
    ip link set ${bname} up
}

add_to_bridge () {
    local bname=$1 mtu=$2 localif=$3

    # set the interface MTU, add it to the bridge, then bring it up
    ip link set ${localif} mtu ${mtu}
    brctl addif ${bname} ${localif}
    ip link set ${localif} up
}

bridge_veth_pair () {
    local bname=$1 mtu=$2 localif=$3

    # create veth pair
    tempif=${localif}c
    ip link add name ${localif} type veth peer name ${tempif}

    # add local interface to bridge
    ip link set ${localif} mtu ${mtu}
    brctl addif ${bname} ${localif} || (
        echo Failed to add ${localif} to ${bname}
        ip link del ${localif} type veth
        exit 1
    )
    ip link set ${localif} up

    # return remote interface
    echo "${tempif}"
}

del_veth_pair () {
    local bname=$1 localif=$2

    # remove local interface from bridge, then delete it
    # the remote interface part of the veth pair will be destroyed automatically
    brctl delif ${bname} ${localif} || echo Failed to remove ${localif} from ${bname}
    ip link del ${localif} type veth
}

destroy_bridge () {
    local bname=$1

    # shut bridge down, then delete it
    ip link set ${bname} down || true
    brctl delbr ${bname}
}
