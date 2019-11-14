#!/bin/bash

# directory where qvm-sync (bash script_ lives
SYNCDIR=/usr/bin

# directory where qsync-send, qsync-receive and qsync-preloader lives
BINDIR=/usr/lib/qubes

# directory where rpc service config lives
RPCDIR=/etc/qubes-rpc

#
# Install the qvm-sync bash script which initiates syncs
#
echo "Installing sender script into $SYNCDIR..."  && \
    sudo cp ./scripts/qvm-sync $SYNCDIR/qvm-sync &&\
    sudo chmod 755 $SYNCDIR/qvm-sync

#
# Install the service that can be invoked via qubes rpc calls
#
echo "Installing receiver into $RPCDIR..."  && \
  sudo cp ./scripts/qubes.Filesync $RPCDIR/qubes.Filesync &&\
  sudo chmod 755 $RPCDIR/qubes.Filesync

#
# Build the binaries, if we have go installed
#
go version && \
  echo "Building binaries..."
  go build ./cmd/qsync-send && go install ./cmd/qsync-send && \
  go build ./cmd/qsync-receive && go install ./cmd/qsync-receive && \
  go build ./cmd/qsync-preloader && go install ./cmd/qsync-preloader

#
# Install the binaries into /usr/lib/qubes
#
echo "Installing binaries into $BINDIR..."
sudo cp qsync-send $BINDIR/ && \
    sudo chmod 0755 $BINDIR/qsync-send
sudo cp qsync-receive $BINDIR/ && \
    sudo chmod 0755 $BINDIR/qsync-receive

#
# The preloader requires suid flag to be set
#
echo "Installing preloader into $BINDIR..."
sudo cp qsync-preloader $BINDIR/ && \
    sudo chmod 4755 $BINDIR/qsync-preloader

echo "Done."
