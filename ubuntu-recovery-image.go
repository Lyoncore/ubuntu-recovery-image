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

var version string
var commit string
var commitstamp string
var build_date string

// setupLoopDevice setup loop device for base image and recovery image.
func setupLoopDevice(recoveryOutputFile string, recoveryNR string) (string, string) {

	log.Printf("[**SETUP_LOOPDEVICE**]")
	basefile, err := os.Open(confBaseImage.value)
	rplib.Checkerr(err)
	defer basefile.Close()
	basefilest, err := basefile.Stat()
	rplib.Checkerr(err)

	log.Printf("fallocate %d bytes for %q\n", basefilest.Size(), confBaseImage.value)
	outputfile, err := os.Create(recoveryOutputFile)
	rplib.Checkerr(err)
	defer outputfile.Close()

	syscall.Fallocate(int(outputfile.Fd()), 0, 0,
		basefilest.Size())
	log.Printf("[setup a loopback device for recovery image %s]", recoveryOutputFile)
	recoveryImageLoop := rplib.Shellcmdoutput(fmt.Sprintf("losetup --find --show %s | xargs basename", recoveryOutputFile))

	log.Printf("[setup a readonly loopback device for base image]")
	baseImageLoop := rplib.Shellcmdoutput(fmt.Sprintf("losetup -r --find --show %s | xargs basename", confBaseImage.value))

	log.Printf("[create %s partition on %s]", recoveryOutputFile, recoveryImageLoop)

	recoveryBegin := 4
	recoverySize, err := strconv.Atoi(confRecoverySize)
	rplib.Checkerr(err)
	recoveryEnd := recoveryBegin + recoverySize

	rplib.Shellexec("parted", "-ms", "-a", "optimal", fmt.Sprintf("/dev/%s", recoveryImageLoop),
		"unit", "MiB",
		"mklabel", "gpt",
		"mkpart", "primary", "fat32", fmt.Sprintf("%d", recoveryBegin), fmt.Sprintf("%d", recoveryEnd),
		"name", recoveryNR, confFsLabel,
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

	if confDevmode {
		developerMode = "--developer-mode"
	} else {
		developerMode = ""
	}

	if confSsh {
		enablessh = "--enable-ssh"
	} else {
		enablessh = ""
	}

	fmt.Printf("Channel: %s\n", confChannel.value)

	if _, err := os.Stat(confBaseImage.value); err == nil {
		fmt.Printf("The file %s exist, remove the file.\n", confBaseImage.value)
		os.Remove(confBaseImage.value)
	}

	rplib.Shellexec(confUdfBinary, confUdfOption, confRelease,
		confStore.opt, confStore.value,
		confDevice.opt, confDevice.value,
		confChannel.opt, confChannel.value,
		confBaseImage.opt, confBaseImage.value,
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
	rplib.Shellexec("mkfs.fat", "-F", "32", "-n", confFsLabel, fmt.Sprintf("/dev/mapper/%sp%s", recoveryImageLoop, recoveryNR))

	tmpDir, err := ioutil.TempDir("", "")
	rplib.Checkerr(err)

	log.Printf("tmpDir:", tmpDir)
	defer os.RemoveAll(tmpDir) // clean up

	recoveryMapperDevice := fmt.Sprintf("/dev/mapper/%sp%s", recoveryImageLoop, recoveryNR)
	recoveryDir := fmt.Sprintf("%s/device/%s/", tmpDir, confFsLabel)
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
	rplib.Shellexec("grub-editenv", fmt.Sprintf("%s/efi/ubuntu/grub/grubenv", recoveryDir), "set", fmt.Sprintf("recoverylabel=%s", confFsLabel))

	rplib.Shellexec("grub-editenv", fmt.Sprintf("%s/efi/ubuntu/grub/grubenv", recoveryDir), "set", "recoverytype=factory_install")

	log.Printf("[setup recovery uuid]")
	recoveryUUID := rplib.Shellexecoutput("blkid", recoveryMapperDevice, "-o", "value", "-s", "UUID")
	rplib.Shellexec("grub-editenv", fmt.Sprintf("%s/efi/ubuntu/grub/grubenv", recoveryDir), "set", fmt.Sprintf("recoveryuuid=%s", recoveryUUID))

	log.Printf("[create/overwrite grub.cfg]")
	rplib.Shellexec("cp", "-f", configFolder+"/data/grub.cfg", fmt.Sprintf("%s/efi/ubuntu/grub/grub.cfg", recoveryDir))

	os.Mkdir(fmt.Sprintf("%s/recovery/", recoveryDir), 0755)
	log.Printf("[add recovery.bin, factory snaps]")
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

	if confOemHookDir != "" {
		log.Printf("[Create oem specific hooks]")
		os.MkdirAll(recoveryDir+"/"+confOemHookDir, os.ModePerm)
	}

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

	Snaps struct {
		Kernel string
		Os     string
		Gadget string
	}

	Configs struct {
		Arch         string
		Baseimage    string
		Recoverytype string
		Recoverysize string
		Release      string
		Store        string
		Device       string
		Channel      string
		Size         string
		Oemhookdir   string
		Oemlogdir    string
	}

	Udf struct {
		Binary string
		Option string
	}

	Debug struct {
		Devmode bool
		Ssh     bool
		Xz      bool
	}

	Recovery struct {
		FsLabel         string `yaml:"filesystem-label"`
		BootPart        string `yaml:"boot-partition"`
		SystembootPart  string `yaml:"systemboot-partition"`
		WritablePart    string `yaml:"writable-partition"`
		BootImage       string `yaml:"boot-image"`
		SystembootImage string `yaml:"systemboot-image"`
		WritableImage   string `yaml:"writable-image"`
	}
}

type Config struct {
	opt, value string
}

var (
	confProject      string
	confKernel       = Config{"--kernel", ""}
	confOs           = Config{"--os", ""}
	confGadget       = Config{"--gadget", ""}
	confBaseImage    = Config{"--output", ""}
	confRecoveryType string
	confRecoverySize string
	confRelease      string
	confStore        = Config{"", ""}
	confDevice       = Config{"", ""}
	confChannel      = Config{"--channel", ""}
	confSize         = Config{"--size", ""}
	confOemHookDir   string
	confOemLogDir    string
	confUdfBinary    string
	confUdfOption    string
	confDevmode      bool
	confSsh          bool
	confXz           bool
	confFsLabel      string
)

func parseConfigs(configs YamlConfigs) bool {
	fmt.Printf("parseConfig ... \n")

	errCount := 0
	if configs.Project == "" {
		log.Println("Parse config.yaml failed, need to specify 'project' field")
		errCount++
	}

	if configs.Snaps.Kernel == "" {
		log.Println("Parse config.yaml failed, need to specify 'snaps -> kernel' field")
		errCount++
	}

	if configs.Snaps.Os == "" {
		log.Println("Parse config.yaml failed, need to specify 'snaps -> os' field")
		errCount++
	}

	if configs.Snaps.Gadget == "" {
		log.Println("Parse config.yaml failed, need to specify 'snaps -> gadget' field")
		errCount++
	}

	if configs.Configs.Baseimage == "" {
		log.Println("Parse config.yaml failed, need to specify 'configs -> baseimage' field")
		errCount++
	}

	if configs.Configs.Recoverytype == "" {
		log.Println("Parse config.yaml failed, need to specify 'configs -> recoverytype' field")
		errCount++
	}

	if configs.Configs.Recoverysize == "" {
		log.Println("Parse config.yaml failed, need to specify 'configs -> recoverysize' field")
		errCount++
	}

	if configs.Configs.Release == "" {
		log.Println("Parse config.yaml failed, need to specify 'configs -> release' field")
		errCount++
	}

	if configs.Configs.Channel == "" {
		log.Println("Parse config.yaml failed, need to specify 'configs -> channel' field")
		errCount++
	}

	if configs.Configs.Size == "" {
		log.Println("Parse config.yaml failed, need to specify 'configs -> size' field")
		errCount++
	}

	if configs.Udf.Binary == "" {
		log.Println("Parse config.yaml failed, need to specify 'udf -> binary' field")
		errCount++
	}

	if configs.Udf.Option == "" {
		log.Println("Parse config.yaml failed, need to specify 'udf -> option' field")
		errCount++
	}

	if configs.Recovery.FsLabel == "" {
		log.Println("Parse config.yaml failed, need to specify 'recovery -> filesystem-label' field")
		errCount++
	}

	if errCount > 0 {
		log.Println("Parse error")
		return true
	}

	confProject = configs.Project
	confKernel.value = configs.Snaps.Kernel
	confOs.value = configs.Snaps.Os
	confGadget.value = configs.Snaps.Gadget
	confBaseImage.value = configs.Configs.Baseimage
	confRecoveryType = configs.Configs.Recoverytype
	confRecoverySize = configs.Configs.Recoverysize
	confRelease = configs.Configs.Release
	confChannel.value = configs.Configs.Channel
	confSize.value = configs.Configs.Size
	confOemHookDir = configs.Configs.Oemhookdir
	confOemLogDir = configs.Configs.Oemlogdir
	confUdfBinary = configs.Udf.Binary
	confUdfOption = configs.Udf.Option
	confDevmode = configs.Debug.Devmode
	confSsh = configs.Debug.Ssh
	confXz = configs.Debug.Xz
	confFsLabel = configs.Recovery.FsLabel

	if configs.Configs.Store != "" {
		confStore.opt = "--store"
		confStore.value = configs.Configs.Store
	}

	if configs.Configs.Device != "" {
		confDevice.opt = "--device"
		confDevice.value = configs.Configs.Device
	}

	confOemHookDir = configs.Configs.Oemhookdir
	confOemLogDir = configs.Configs.Oemlogdir

	return false
}

func printConfigs() {
	fmt.Printf("Configs from yaml file\n")
	fmt.Println("-----------------------------------------------")
	fmt.Println("project: ", confProject)
	fmt.Println("kernel: ", confKernel)
	fmt.Println("os: ", confOs)
	fmt.Println("gadget: ", confGadget)
	fmt.Println("baseimage: ", confBaseImage)
	fmt.Println("recoverytype: ", confRecoveryType)
	fmt.Println("recoverysize: ", confRecoverySize)
	fmt.Println("release: ", confRelease)
	fmt.Println("store: ", confStore)
	fmt.Println("device: ", confDevice)
	fmt.Println("channel: ", confChannel)
	fmt.Println("size: ", confSize)
	fmt.Println("oemhookdir: ", confOemHookDir)
	fmt.Println("oemlogdir: ", confOemLogDir)
	fmt.Println("udf binary: ", confUdfBinary)
	fmt.Println("udf option: ", confUdfOption)
	fmt.Println("devmode: ", confDevmode)
	fmt.Println("ssh: ", confSsh)
	fmt.Println("xz: ", confXz)
	fmt.Println("fslabel: ", confFsLabel)
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

	errParse := parseConfigs(configs)
	printConfigs()

	if errParse {
		return
	}

	commitstampInt64, _ := strconv.ParseInt(commitstamp, 10, 64)
	log.Printf("Version: %v, Commit: %v, Commit date: %v\n", version, commit, time.Unix(commitstampInt64, 0).UTC())

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	log.Printf("[Setup project for %s]", confProject)

	// Create base image or recovery image or both according to RecoveryType
	if confRecoveryType == "base" || confRecoveryType == "full" {
		createBaseImage()
		if confRecoveryType == "base" {
			log.Printf("[Create base image %s only]", confBaseImage.value)
			os.Exit(0)
		}
	} else if confRecoveryType == "recovery" {
		log.Printf("[Base image is %s]", confBaseImage.value)
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
	if confXz {
		compressXZImage(recoveryOutputFile)
	}
}
