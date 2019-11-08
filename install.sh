#!/bin/bash

#
# Install the qvm-sync bash script which initiates syncs
#
echo "Installing sender..."  && \
    sudo cp ./scripts/qvm-sync /usr/bin/qvm-sync &&\
    sudo chmod 755 /usr/bin/qvm-sync

#
# Install the service that can be invoked via qubes rpc calls
#
echo "Installing receiver..."  && \
  sudo cp ./scripts/qubes.Filesync /etc/qubes-rpc/qubes.Filesync &&\
  sudo chmod 755 /etc/qubes-rpc/qubes.Filesync

#
# Build the binaries, if we have go installed
#
go version && \
  echo "Building binaries..."
  go build ./cmd/qsync-send && go install ./cmd/qsync-send && \
  go build ./cmd/qsync-receive && go install ./cmd/qsync-receive && \
  go build ./cmd/qsync-preloader && go install ./cmd/qsync-preloader && \

#
# Install the binaries into /usr/lib/qubes
#
echo "Installing binaries"
sudo cp qsync-send /usr/lib/qubes/ && \
    sudo chmod 0755 /usr/lib/qubes/qsync-send
sudo cp qsync-receive /usr/lib/qubes/ && \
    sudo chmod 0755 /usr/lib/qubes/qsync-receive

#
# The preloader requires suid flag to be set
#
echo "Installing preloader"
sudo cp qsync-preloader /usr/lib/qubes/ && \
    sudo chmod 4755 /usr/lib/qubes/qsync-preloader

echo "Done!"
