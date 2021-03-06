# set author and base
FROM fedora:22
MAINTAINER Sid Carter <nospam@sidcarter.com>

LABEL version="0.1"
LABEL description="Fedora 22 docker image for heketi"

RUN groupadd -r heketi && useradd -r -g heketi heketi

# let's setup all the necessary environment variables
ENV BUILD_HOME=/build
ENV GOPATH=$BUILD_HOME/golang
ENV PATH=$GOPATH/bin:$PATH
ENV HEKETIC_CONF_DIR=/etc/heketi

# install dependencies, build and cleanup
RUN mkdir $BUILD_HOME $GOPATH $HEKETI_CONF_DIR && \
    dnf -q -y install golang git && \
    dnf -q -y install make && \
    dnf -q -y clean all && \
    cd $BUILD_HOME && \
    go get github.com/heketi/heketi && \
    go get github.com/robfig/glock && \
    glock sync github.com/heketi/heketi && \
    cd $GOPATH/src/github.com/heketi/heketi && make && \
    cp heketi /usr/bin/heketi && \
    cd && rm -rf $BUILD_HOME && \
    dnf -q -y remove git golang make && \
    dnf -q -y autoremove && \
    dnf -q -y clean all

# post install config and volume setup
ADD ./heketi.json /etc/heketi/heketi.json
VOLUME /etc/heketi

RUN mkdir /var/lib/heketi && \
    chown -R heketi:heketi /var/lib/heketi
VOLUME /var/lib/heketi

# expose port, set user and set entrypoint with config option
USER heketi
ENTRYPOINT ["/usr/bin/heketi"]
EXPOSE 8080

CMD ["-config=/etc/heketi/heketi.json"]
