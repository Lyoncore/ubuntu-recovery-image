# ubuntu-recovery-image

<pre>
Build the recovery image

1. Edit .bashrc, add following environment variables.
cat <<EOF >> ~/.bashrc
#setup GOPATH
export GOPATH=${HOME}/gocode
export PATH="$PATH:$GOPATH/bin"
EOF

2. Get libraries
$. ~/.bashrc
$git clone https://github.com/Lyoncore/ubuntu-recovery-image.git
$go get launchpad.net/godeps
$godeps -t -u dependencies.tsv
$go run build.go build
$sudo ./ubuntu-recovery-image

3.run the image in kvm
$sudo apt install -y qemu-kvm ovmf
$sudo kvm -m 512 -bios /usr/share/ovmf/OVMF.fd ubuntu-recovery.img -net nic -net user

4.To test in KVM with vnc using vinagre, you could use the following commands to start vnc on port 5901.
$sudo kvm -m 512 -bios /usr/share/ovmf/OVMF.fd -vnc 0.0.0.0:1 ubuntu-recovery.img -net nic -net user

</pre>
