sudo docker run --privileged --rm tonistiigi/binfmt --install all

sudo docker run --platform amd64 -ti -v ${1}go/src/gitlab.jiagouyun.com/cloudcare-tools/datakit:/root/go/src/gitlab.jiagouyun.com/cloudcare-tools/datakit \
    vircoys/datakit-developer:1.5 /bin/bash
