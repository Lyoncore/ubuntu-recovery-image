package main

import (
	"fmt"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"syscall"
	"time"
)

import rplib "github.com/Lyoncore/ubuntu-recovery-rplib"
import rpoem "github.com/Lyoncore/ubuntu-recovery-rpoem"

var version string
var commit string
var commitstamp string
var build_date string

// setupLoopDevice setup loop device for base image and recovery image.
func setupLoopDevice(recoveryOutputFile string, recoveryNR string) (string, string) {

	log.Printf("[**SETUP_LOOPDEVICE**]")
	basefile, err := os.Open(confBaseImageName.value)
	rplib.Checkerr(err)
	defer basefile.Close()
	basefilest, err := basefile.Stat()
	rplib.Checkerr(err)

	log.Printf("fallocate %d bytes for %q\n", basefilest.Size(), confBaseImageName.value)
	outputfile, err := os.Create(recoveryOutputFile)
	rplib.Checkerr(err)
	defer outputfile.Close()

	syscall.Fallocate(int(outputfile.Fd()), 0, 0,
		basefilest.Size())
	log.Printf("[setup a loopback device for recovery image %s]", recoveryOutputFile)
	recoveryImageLoop := rplib.Shellcmdoutput(fmt.Sprintf("losetup --find --show %s | xargs basename", recoveryOutputFile))

	log.Printf("[setup a readonly loopback device for base image]")
	baseImageLoop := rplib.Shellcmdoutput(fmt.Sprintf("losetup -r --find --show %s | xargs basename", confBaseImageName.value))

	log.Printf("[create %s partition on %s]", recoveryOutputFile, recoveryImageLoop)

	recoveryBegin := 4
	recoverySize, err := strconv.Atoi(confRecoverySize)
	rplib.Checkerr(err)
	recoveryEnd := recoveryBegin + recoverySize

	rplib.Shellexec("parted", "-ms", "-a", "optimal", fmt.Sprintf("/dev/%s", recoveryImageLoop),
		"unit", "MiB",
		"mklabel", "gpt",
		"mkpart", "primary", "fat32", fmt.Sprintf("%d", recoveryBegin), fmt.Sprintf("%d", recoveryEnd),
		"name", recoveryNR, rpoem.FilesystemLabel(),
		"set", recoveryNR, "boot", "on",
		"print")

	return baseImageLoop, recoveryImageLoop
}

func findSnap(folder, name string) string {
	paths, err := filepath.Glob(fmt.Sprintf("%s/%s_*.snap", folder, name))
	rplib.Checkerr(err)
	if 1 != len(paths) {
		log.Print(paths)
		log.Panic("Should have one and only one specified snap")
	}
	path := paths[0]
	log.Printf("snap path:", path)
	return path
}

func setupInitrd(initrdImagePath string, tmpDir string, recoveryDir string) {
	log.Printf("[SETUP_INITRD]")

	initrdTmpDir := fmt.Sprintf("%s/misc/initrd/", tmpDir)
	log.Printf("[setup %s/misc/initrd]", tmpDir)
	err := os.MkdirAll(initrdTmpDir, 0755)
	rplib.Checkerr(err)
	defer os.RemoveAll(initrdTmpDir)

	log.Printf("[processiing kernel snaps]")
	kernelsnapTmpDir := fmt.Sprintf("%s/misc/kernel-snap", tmpDir)
	err = os.MkdirAll(kernelsnapTmpDir, 0755)
	rplib.Checkerr(err)
	defer os.RemoveAll(kernelsnapTmpDir)

	log.Printf("[copy kernel snap to recoveryDir]")
	kernelSnapPath := findSnap(fmt.Sprintf("%s/image/writable/system-data/var/lib/snapd/snaps/", tmpDir), confKernel.value)

	rplib.Shellexec("mount", kernelSnapPath, kernelsnapTmpDir)
	defer syscall.Unmount(kernelsnapTmpDir, 0)

	log.Printf("[unxz initrd in kernel snap]")
	unxzInitrdCmd := fmt.Sprintf("unxz < %s/initrd.img | (cd %s; cpio -i )", kernelsnapTmpDir, initrdTmpDir)
	_ = rplib.Shellcmdoutput(unxzInitrdCmd)

	kerVer := rplib.Shellcmdoutput(fmt.Sprintf("basename %s/lib/modules/*", initrdTmpDir))

	err = os.MkdirAll(fmt.Sprintf("%s/lib/modules/%s/kernel/fs/nls", initrdTmpDir, kerVer), 0755)
	rplib.Checkerr(err)

	rplib.Shellexec("cp", fmt.Sprintf("%s/lib/modules/%s/kernel/fs/nls/nls_iso8859-1.ko", kernelsnapTmpDir, kerVer), fmt.Sprintf("%s/lib/modules/%s/kernel/fs/nls/nls_iso8859-1.ko", initrdTmpDir, kerVer))
	rplib.Shellexec("depmod", "-a", "-b", initrdTmpDir, kerVer)
	_ = rplib.Shellcmdoutput(fmt.Sprintf("rm -f %s/lib/modules/*/modules.*map", initrdTmpDir))

	log.Printf("[modify initrd ORDER file]")
	orderFile := fmt.Sprintf("%s/scripts/local-premount/ORDER", initrdTmpDir)
	orderData, err := ioutil.ReadFile(orderFile)
	rplib.Checkerr(err)

	orderDataInsertFront := []byte("[ -e /conf/param.conf ] && . /conf/param.conf\n/scripts/local-premount/00_recovery $@\n")
	err = ioutil.WriteFile(orderFile, append(orderDataInsertFront[:], orderData[:]...), 0755)
	rplib.Checkerr(err)

	log.Printf("[create initrd recovery script]")
	recoveryInitrdScript, err := ioutil.ReadFile("00_recovery")
	rplib.Checkerr(err)
	err = ioutil.WriteFile(fmt.Sprintf("%s/scripts/local-premount/00_recovery", initrdTmpDir), recoveryInitrdScript, 0755)
	rplib.Checkerr(err)

	log.Printf("[recreate initrd]")
	_ = rplib.Shellcmdoutput(fmt.Sprintf("( cd %s; find | cpio --quiet -o -H newc ) | xz -c9 --check=crc32 > %s", initrdTmpDir, initrdImagePath))
}

func createBaseImage() {
	var developerMode, enablessh string

	if confDevmode == "enable" {
		developerMode = "--developer-mode"
	} else {
		developerMode = ""
	}

	if confSsh == "enable" {
		enablessh = "--enable-ssh"
	} else {
		enablessh = ""
	}

	fmt.Printf("Channel: %s\n", confChannel.value)

	if _, err := os.Stat(confBaseImageName.value); err == nil {
		fmt.Printf("The file %s exist, remove the file.\n", confBaseImageName.value)
		os.Remove(confBaseImageName.value)
	}

	rplib.Shellexec(confUdfBinary, confUdfOption, confRelease,
		confStore.opt, confStore.value,
		confDevice.opt, confDevice.value,
		confChannel.opt, confChannel.value,
		confBaseImageName.opt, confBaseImageName.value,
		enablessh,
		confSize.opt, confSize.value,
		developerMode,
		confKernel.opt, confKernel.value,
		confOs.opt, confOs.value,
		confGadget.opt, confGadget.value)
}

func createRecoveryImage(recoveryNR string, recoveryOutputFile string, configFolder string) {
	baseImageLoop, recoveryImageLoop := setupLoopDevice(recoveryOutputFile, recoveryNR)
	defer rplib.Shellcmd(fmt.Sprintf("losetup -d /dev/%s", baseImageLoop))
	defer rplib.Shellcmd(fmt.Sprintf("losetup -d /dev/%s", recoveryImageLoop))
	log.Printf("[base image loop:%s,recovery image loop: %s created]\n", baseImageLoop, recoveryImageLoop)

	// Create device maps from partition tables
	log.Printf("[kpartx]")
	rplib.Shellexec("kpartx", "-a", fmt.Sprintf("/dev/%s", baseImageLoop))
	rplib.Shellexec("kpartx", "-a", fmt.Sprintf("/dev/%s", recoveryImageLoop))
	rplib.Shellexec("udevadm", "settle")
	// Delete device maps
	defer rplib.Shellexec("udevadm", "settle")
	defer rplib.Shellexec("kpartx", "-d", fmt.Sprintf("/dev/%s", recoveryImageLoop))
	defer rplib.Shellexec("kpartx", "-d", fmt.Sprintf("/dev/%s", baseImageLoop))

	// TODO: rewritten with launchpad/goget-ubuntu-touch/DiskImage image.Create
	log.Printf("[mkfs.fat]")
	rplib.Shellexec("mkfs.fat", "-F", "32", "-n", rpoem.FilesystemLabel(), fmt.Sprintf("/dev/mapper/%sp%s", recoveryImageLoop, recoveryNR))

	tmpDir, err := ioutil.TempDir("", "")
	rplib.Checkerr(err)

	log.Printf("tmpDir:", tmpDir)
	defer os.RemoveAll(tmpDir) // clean up

	recoveryMapperDevice := fmt.Sprintf("/dev/mapper/%sp%s", recoveryImageLoop, recoveryNR)
	recoveryDir := fmt.Sprintf("%s/device/%s/", tmpDir, rpoem.FilesystemLabel())
	log.Printf("[mkdir %s]", recoveryDir)

	err = os.MkdirAll(recoveryDir, 0755)
	rplib.Checkerr(err)

	log.Printf("[mount device %s on recovery dir %s]", recoveryMapperDevice, recoveryDir)
	err = syscall.Mount(recoveryMapperDevice, recoveryDir, "vfat", 0, "")
	rplib.Checkerr(err)
	defer syscall.Unmount(recoveryDir, 0)

	baseMapperDeviceGlobName := fmt.Sprintf("/dev/mapper/%s*", baseImageLoop)
	baseMapperDeviceArray, err := filepath.Glob(baseMapperDeviceGlobName)
	rplib.Checkerr(err)

	for _, part := range baseMapperDeviceArray {
		label := rplib.Shellexecoutput("blkid", part, "-o", "value", "-s", "LABEL")
		if match, _ := regexp.MatchString("system-boot|system-a|writable", label); match {
			log.Printf("matched")
			baseDir := fmt.Sprintf("%s/image/%s", tmpDir, label)
			err := os.MkdirAll(baseDir, 0755)
			rplib.Checkerr(err)
			defer os.RemoveAll(baseDir) // clean up

			log.Printf("[mount device %s on base image dir %s]", part, label)
			fstype := rplib.Shellexecoutput("blkid", part, "-o", "value", "-s", "TYPE")
			err = syscall.Mount(part, baseDir, fstype, 0, "")
			rplib.Checkerr(err)

			defer syscall.Unmount(baseDir, 0)
		}
	}

	log.Printf("[deploy default efi bootdir]")

	rplib.Shellexec("cp", "-ar", fmt.Sprintf("%s/image/system-boot/efi", tmpDir), recoveryDir)
	err = os.Remove(fmt.Sprintf("%s/efi/ubuntu/grub/grubenv", recoveryDir))
	rplib.Checkerr(err)

	log.Printf("[create grubenv for switching between core and recovery system]")
	rplib.Shellexec("grub-editenv", fmt.Sprintf("%s/efi/ubuntu/grub/grubenv", recoveryDir), "create")
	rplib.Shellexec("grub-editenv", fmt.Sprintf("%s/efi/ubuntu/grub/grubenv", recoveryDir), "set", "firstfactoryrestore=no")
	rplib.Shellexec("grub-editenv", fmt.Sprintf("%s/efi/ubuntu/grub/grubenv", recoveryDir), "set", fmt.Sprintf("recoverylabel=%s", rpoem.FilesystemLabel()))

	rplib.Shellexec("grub-editenv", fmt.Sprintf("%s/efi/ubuntu/grub/grubenv", recoveryDir), "set", "recoverytype=factory_install")

	log.Printf("[setup recovery uuid]")
	recoveryUUID := rplib.Shellexecoutput("blkid", recoveryMapperDevice, "-o", "value", "-s", "UUID")
	rplib.Shellexec("grub-editenv", fmt.Sprintf("%s/efi/ubuntu/grub/grubenv", recoveryDir), "set", fmt.Sprintf("recoveryuuid=%s", recoveryUUID))

	log.Printf("[create/overwrite grub.cfg]")
	rplib.Shellexec("cp", "-f", configFolder+"/data/grub.cfg", fmt.Sprintf("%s/efi/ubuntu/grub/grub.cfg", recoveryDir))

	os.Mkdir(fmt.Sprintf("%s/recovery/", recoveryDir), 0755)
	log.Printf("[add recovery.bin, factory snaps]")
	rplib.Shellexec("go", "build", "-o", configFolder+"/data/bin/recovery.bin", configFolder+"/src/recovery.bin.go")
	rplib.Shellexec("cp", "-r", configFolder+"/data/factory", configFolder+"/data/bin", fmt.Sprintf("%s/recovery/", recoveryDir))

	log.Printf("add system-data and writable tarball")
	workingDir, err := os.Getwd()
	rplib.Checkerr(err)

	err = os.Chdir(fmt.Sprintf("%s/image/system-boot/", tmpDir))
	rplib.Checkerr(err)
	rplib.Shellexec("tar", "--xattrs", "-Jcpf", fmt.Sprintf("%s/recovery/factory/system-boot.tar.xz", recoveryDir), ".")

	err = os.Chdir(fmt.Sprintf("%s/image/writable/", tmpDir))
	rplib.Checkerr(err)
	rplib.Shellexec("tar", "--xattrs", "-Jcpf", fmt.Sprintf("%s/recovery/factory/writable.tar.xz", recoveryDir), ".")

	os.Chdir(workingDir)

	// copy kernel, gadget, os snap
	kernelSnap := findSnap(fmt.Sprintf("%s/image/writable/system-data/var/lib/snapd/snaps/", tmpDir), confKernel.value)
	rplib.Shellexec("cp", "-f", kernelSnap, fmt.Sprintf("%s/kernel.snap", recoveryDir))
	gadgetSnap := findSnap(fmt.Sprintf("%s/image/writable/system-data/var/lib/snapd/snaps/", tmpDir), confGadget.value)
	rplib.Shellexec("cp", "-f", gadgetSnap, fmt.Sprintf("%s/gadget.snap", recoveryDir))
	osSnap := findSnap(fmt.Sprintf("%s/image/writable/system-data/var/lib/snapd/snaps/", tmpDir), confOs.value)
	rplib.Shellexec("cp", "-f", osSnap, fmt.Sprintf("%s/os.snap", recoveryDir))

	log.Printf("[Create oem specific hooks]")
	rpoem.CreateHookDir(recoveryDir)

	log.Printf("[setup initrd.img and vmlinuz]")
	initrdImagePath := fmt.Sprintf("%s/initrd.img", recoveryDir)
	setupInitrd(initrdImagePath, tmpDir, recoveryDir)
}

func compressXZImage(imageFile string) {
	log.Printf("[compress image: %s.xz]", imageFile)
	rplib.Shellexec("xz", "-0", imageFile)
}

// configs from yaml file
type YamlConfigs struct {
	Project string
	Snaps   map[string][]string
	Configs map[string][]string
	Udf     map[string][]string
	Debug   map[string][]string
}

type Config struct {
	opt, value string
}

var (
	confProject       = "ubuntucore"
	confKernel        = Config{"--kernel", "canonical-pc-linux"}
	confOs            = Config{"--os", "ubuntu-core"}
	confGadget        = Config{"--gadget", "canonical-pc"}
	confBaseImageName = Config{"--output", "base.img"}
	confRecoveryType  = "full"
	confRecoverySize  = "768"
	confRelease       = "16"
	confStore         = Config{"", ""}
	confDevice        = Config{"", ""}
	confChannel       = Config{"--channel", "stable"}
	confSize          = Config{"--size", "4"}
	confUdfBinary     = "./ubuntu-device-flash"
	confUdfOption     = "core"
	confDevmode       = "enable"
	confSsh           = "enable"
	confXz            = "enable"
)

func parseConfigs(configs YamlConfigs) {
	fmt.Printf("parseConfig ... \n")

	if configs.Project != "" {
		confProject = configs.Project
	}

	if configs.Snaps["kernel"] != nil {
		confKernel.value = configs.Snaps["kernel"][0]
	}

	if configs.Snaps["os"] != nil {
		confOs.value = configs.Snaps["os"][0]
	}

	if configs.Snaps["gadget"] != nil {
		confGadget.value = configs.Snaps["gadget"][0]
	}

	if configs.Configs["baseimagename"] != nil {
		confBaseImageName.value = configs.Configs["baseimagename"][0]
	}

	if configs.Configs["recoverytype"] != nil {
		confRecoveryType = configs.Configs["recoverytype"][0]
	}

	if configs.Configs["recoverysize"] != nil {
		confRecoverySize = configs.Configs["recoverysize"][0]
	}

	if configs.Configs["release"] != nil {
		confRelease = configs.Configs["release"][0]
	}

	if configs.Configs["store"] != nil {
		confStore.opt = "--store"
		confStore.value = configs.Configs["store"][0]
	}

	if configs.Configs["device"] != nil {
		confDevice.opt = "--device"
		confDevice.value = configs.Configs["device"][0]
	}

	if configs.Configs["channel"] != nil {
		confChannel.value = configs.Configs["channel"][0]
	}

	if configs.Configs["size"] != nil {
		confChannel.value = configs.Configs["channel"][0]
	}

	if configs.Udf["binary"] != nil {
		confUdfBinary = configs.Udf["binary"][0]
	}

	if configs.Udf["option"] != nil {
		confUdfOption = configs.Udf["option"][0]
	}

	if configs.Debug["devmode"] != nil {
		confDevmode = configs.Debug["devmode"][0]
	}

	if configs.Debug["ssh"] != nil {
		confSsh = configs.Debug["ssh"][0]
	}

	if configs.Debug["xz"] != nil {
		confXz = configs.Debug["xz"][0]
	}
}

func printConfigs() {
	fmt.Printf("Configs from yaml file\n")
	fmt.Println("-----------------------------------------------")
	fmt.Printf("project: %s\n", confProject)
	fmt.Printf("kernel: %s\n", confKernel)
	fmt.Printf("os: %s\n", confOs)
	fmt.Printf("gadget: %s\n", confGadget)
	fmt.Printf("baseimagename: %s\n", confBaseImageName)
	fmt.Printf("recoverytype: %s\n", confRecoveryType)
	fmt.Printf("recoverysize: %s\n", confRecoverySize)
	fmt.Printf("release: %s\n", confRelease)
	fmt.Printf("store: %s\n", confStore)
	fmt.Printf("device: %s\n", confDevice)
	fmt.Printf("channel: %s\n", confChannel)
	fmt.Printf("size: %s\n", confSize)
	fmt.Printf("udf binary: %s\n", confUdfBinary)
	fmt.Printf("udf option: %s\n", confUdfOption)
	fmt.Printf("devmode: %s\n", confDevmode)
	fmt.Printf("ssh: %s\n", confSsh)
	fmt.Printf("xz: %s\n", confXz)
	fmt.Println("-----------------------------------------------")
}

func main() {
	var configs YamlConfigs
	var configFolder string

	args := os.Args
	numargs := len(args)

	if numargs == 2 {
		configFolder = args[1]
	} else {
		log.Fatal("You need to provide config folder.")
	}

	configFile := configFolder + "/config.yaml"
	fmt.Printf("Loading config file %s ...\n", configFile)
	filename, _ := filepath.Abs(configFile)
	yamlFile, err := ioutil.ReadFile(filename)

	if err != nil {
		fmt.Printf("Can not load %s\n", configFile)
		panic(err)
	}

	err = yaml.Unmarshal(yamlFile, &configs)
	if err != nil {
		fmt.Printf("Parse %s failed\n", configFile)
		panic(err)
	}

	parseConfigs(configs)
	printConfigs()

	commitstampInt64, _ := strconv.ParseInt(commitstamp, 10, 64)
	log.Printf("Version: %v, Commit: %v, Commit date: %v\n", version, commit, time.Unix(commitstampInt64, 0).UTC())

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	log.Printf("[Setup project for %s]", confProject)
	rpoem.InitProject(confProject)

	// Create base image or recovery image or both according to RecoveryType
	if confRecoveryType == "base" || confRecoveryType == "full" {
		createBaseImage()
		if confRecoveryType == "base" {
			log.Printf("[Create base image %s only]", confBaseImageName.value)
			os.Exit(0)
		}
	} else if confRecoveryType == "recovery" {
		log.Printf("[Base image is %s]", confBaseImageName.value)
	} else {
		fmt.Printf("%q is not valid type.\n", confRecoveryType)
		os.Exit(2)
	}

	recoveryNR := "1"

	log.Printf("[start create recovery image with skipxz options: %s.\n]", confXz)

	todayTime := time.Now()
	todayDate := fmt.Sprintf("%d%02d%02d", todayTime.Year(), todayTime.Month(), todayTime.Day())
	recoveryOutputFile := confProject + "-" + todayDate + "-0.img"

	createRecoveryImage(recoveryNR, recoveryOutputFile, configFolder)
	if confXz == "enable" {
		compressXZImage(recoveryOutputFile)
	}
}
