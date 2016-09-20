# ubuntu-recovery-image

<pre>
Build the recovery image

1. Edit .bashrc, add following environment variables.
$sudo apt install -y git bzr golang-go kpartx
$cat <<EOF >> ~/.bashrc
#setup GOPATH
export GOPATH=${HOME}/gocode
export PATH="$PATH:$GOPATH/bin"
EOF
$. ~/.bashrc
$go get launchpad.net/godeps

2. Build ubuntu-recovery-image
$go get github.com/Lyoncore/ubuntu-recovery-image
$cd $GOPATH/src/github.com/Lyoncore/ubuntu-recovery-image
$godeps -t -u dependencies.tsv
$go run build.go build
$cd ../

3. Get config and build image
$git clone https://github.com/Lyoncore/generic-amd64-config.git
$cd generic-amd64-config/
$go run build.go build
$sh cook-image.sh
$sudo $GOPATH/bin/ubuntu-recovery-image

4. Run the image in kvm
$sudo apt install -y qemu-kvm ovmf
$sudo kvm -m 512 -bios /usr/share/ovmf/OVMF.fd ubuntu-recovery.img -net nic -net user

5. To test in KVM with vnc using vinagre, you could use the following commands to start vnc on port 5901.
$sudo kvm -m 512 -bios /usr/share/ovmf/OVMF.fd -vnc 0.0.0.0:1 ubuntu-recovery.img -net nic -net user

</pre>

## Sign Serial
```bash
$ go run build.go build
$ ./signserial config-example.yaml
```
## generate mock assertions
```bash
$ go run build.go build
$ ./mockSerialGen config-example.yaml
```
