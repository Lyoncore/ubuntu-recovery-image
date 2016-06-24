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
	log.Printf("[SETUP_LOOPDEVICE]")
	basefile, err := os.Open(configs.Configs.BaseImage)
	rplib.Checkerr(err)
	defer basefile.Close()
	basefilest, err := basefile.Stat()
	rplib.Checkerr(err)

	log.Printf("fallocate %d bytes for %q\n", basefilest.Size(), configs.Configs.BaseImage)
	outputfile, err := os.Create(recoveryOutputFile)
	rplib.Checkerr(err)
	defer outputfile.Close()

	syscall.Fallocate(int(outputfile.Fd()), 0, 0, basefilest.Size())
	log.Printf("[setup a loopback device for recovery image %s]", recoveryOutputFile)
	recoveryImageLoop := rplib.Shellcmdoutput(fmt.Sprintf("losetup --find --show %s | xargs basename", recoveryOutputFile))

	log.Printf("[setup a readonly loopback device for base image]")
	baseImageLoop := rplib.Shellcmdoutput(fmt.Sprintf("losetup -r --find --show %s | xargs basename", configs.Configs.BaseImage))

	log.Printf("[create %s partition on %s]", recoveryOutputFile, recoveryImageLoop)

	recoveryBegin := 4
	recoverySize, err := strconv.Atoi(configs.Configs.RecoverySize)
	rplib.Checkerr(err)
	recoveryEnd := recoveryBegin + recoverySize

	rplib.Shellexec("parted", "-ms", "-a", "optimal", fmt.Sprintf("/dev/%s", recoveryImageLoop),
		"unit", "MiB",
		"mklabel", "gpt",
		"mkpart", "primary", "fat32", fmt.Sprintf("%d", recoveryBegin), fmt.Sprintf("%d", recoveryEnd),
		"name", recoveryNR, configs.Recovery.FsLabel,
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
	kernelSnapPath := findSnap(fmt.Sprintf("%s/image/writable/system-data/var/lib/snapd/snaps/", tmpDir), configs.Snaps.Kernel)

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
	fmt.Printf("Create base image, channel: %s\n", configs.Configs.Channel)

	if _, err := os.Stat(configs.Configs.BaseImage); err == nil {
		fmt.Printf("The file %s exist, remove the file.\n", configs.Configs.BaseImage)
		os.Remove(configs.Configs.BaseImage)
	}

	rplib.Shellexec(configs.Udf.Binary, configs.Udf.Option, configs.Configs.Release,
		optStore, configs.Configs.Store,
		optDevice, configs.Configs.Device,
		optChannel, configs.Configs.Channel,
		optBaseImage, configs.Configs.BaseImage,
		optSsh,
		optSize, configs.Configs.Size,
		optDevmode,
		optKernel, configs.Snaps.Kernel,
		optOs, configs.Snaps.Os,
		optGadget, configs.Snaps.Gadget)
}

func createRecoveryImage(recoveryNR string, recoveryOutputFile string, configFolder string) {
	// Setup loop devices
	baseImageLoop, recoveryImageLoop := setupLoopDevice(recoveryOutputFile, recoveryNR)
	// Delete loop devices
	defer rplib.Shellcmd(fmt.Sprintf("losetup -d /dev/%s", baseImageLoop))
	defer rplib.Shellcmd(fmt.Sprintf("losetup -d /dev/%s", recoveryImageLoop))
	log.Printf("[base image loop:%s, recovery image loop: %s created]\n", baseImageLoop, recoveryImageLoop)

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
	rplib.Shellexec("mkfs.fat", "-F", "32", "-n", configs.Recovery.FsLabel, fmt.Sprintf("/dev/mapper/%sp%s", recoveryImageLoop, recoveryNR))

	tmpDir, err := ioutil.TempDir("", "")
	rplib.Checkerr(err)

	log.Printf("tmpDir:", tmpDir)
	defer os.RemoveAll(tmpDir) // clean up

	recoveryMapperDevice := fmt.Sprintf("/dev/mapper/%sp%s", recoveryImageLoop, recoveryNR)
	recoveryDir := fmt.Sprintf("%s/device/%s/", tmpDir, configs.Recovery.FsLabel)
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
	rplib.Shellexec("grub-editenv", fmt.Sprintf("%s/efi/ubuntu/grub/grubenv", recoveryDir), "set", fmt.Sprintf("recoverylabel=%s", configs.Recovery.FsLabel))

	rplib.Shellexec("grub-editenv", fmt.Sprintf("%s/efi/ubuntu/grub/grubenv", recoveryDir), "set", "recoverytype=factory_install")

	log.Printf("[setup recovery uuid]")
	recoveryUUID := rplib.Shellexecoutput("blkid", recoveryMapperDevice, "-o", "value", "-s", "UUID")
	rplib.Shellexec("grub-editenv", fmt.Sprintf("%s/efi/ubuntu/grub/grubenv", recoveryDir), "set", fmt.Sprintf("recoveryuuid=%s", recoveryUUID))

	log.Printf("[create/overwrite grub.cfg]")
	rplib.Shellexec("cp", "-f", configFolder+"/data/grub.cfg", fmt.Sprintf("%s/efi/ubuntu/grub/grub.cfg", recoveryDir))

	os.Mkdir(fmt.Sprintf("%s/recovery/", recoveryDir), 0755)
	log.Printf("[add folder bin/]")
	rplib.Shellexec("cp", "-r", configFolder+"/data/bin", fmt.Sprintf("%s/recovery/", recoveryDir))
	log.Printf("[add factory snaps folder: factory/]")
	rplib.Shellexec("cp", "-r", configFolder+"/data/factory", fmt.Sprintf("%s/recovery/", recoveryDir))
	log.Printf("[add folder assertions/]")
	rplib.Shellexec("cp", "-r", configFolder+"/data/assertions", fmt.Sprintf("%s/recovery/", recoveryDir))

	if configs.Recovery.SystembootImage != "" && configs.Recovery.WritableImage != "" {
		log.Printf("Copy user provided system-boot (%s) and writable (%s) images to %s/recovery/factory directory\n",
			configs.Recovery.SystembootImage, configs.Recovery.WritableImage, recoveryDir)

		rplib.Shellexec("cp", configFolder+"/"+configs.Recovery.SystembootImage, fmt.Sprintf("%s/recovery/factory/", recoveryDir))
		rplib.Shellexec("cp", configFolder+"/"+configs.Recovery.WritableImage, fmt.Sprintf("%s/recovery/factory/", recoveryDir))
	} else {
		log.Printf("add system-data and writable tarball from base image")

		workingDir, err := os.Getwd()
		rplib.Checkerr(err)

		err = os.Chdir(fmt.Sprintf("%s/image/system-boot/", tmpDir))
		rplib.Checkerr(err)
		rplib.Shellexec("tar", "--xattrs", "-Jcpf", fmt.Sprintf("%s/recovery/factory/system-boot.tar.xz", recoveryDir), ".")

		err = os.Chdir(fmt.Sprintf("%s/image/writable/", tmpDir))
		rplib.Checkerr(err)
		rplib.Shellexec("tar", "--xattrs", "-Jcpf", fmt.Sprintf("%s/recovery/factory/writable.tar.xz", recoveryDir), ".")

		os.Chdir(workingDir)
	}

	// copy kernel, gadget, os snap
	kernelSnap := findSnap(fmt.Sprintf("%s/image/writable/system-data/var/lib/snapd/snaps/", tmpDir), configs.Snaps.Kernel)
	rplib.Shellexec("cp", "-f", kernelSnap, fmt.Sprintf("%s/kernel.snap", recoveryDir))
	gadgetSnap := findSnap(fmt.Sprintf("%s/image/writable/system-data/var/lib/snapd/snaps/", tmpDir), configs.Snaps.Gadget)
	rplib.Shellexec("cp", "-f", gadgetSnap, fmt.Sprintf("%s/gadget.snap", recoveryDir))
	osSnap := findSnap(fmt.Sprintf("%s/image/writable/system-data/var/lib/snapd/snaps/", tmpDir), configs.Snaps.Os)
	rplib.Shellexec("cp", "-f", osSnap, fmt.Sprintf("%s/os.snap", recoveryDir))

	if configs.Configs.OemHookDir != "" {
		log.Printf("[Create oem specific hook directories]")
		os.MkdirAll(recoveryDir+"/"+configs.Configs.OemHookDir, os.ModePerm)
	}

	log.Printf("[setup initrd.img and vmlinuz]")
	initrdImagePath := fmt.Sprintf("%s/initrd.img", recoveryDir)
	setupInitrd(initrdImagePath, tmpDir, recoveryDir)
}

func compressXZImage(imageFile string) {
	log.Printf("[compress image: %s.xz]", imageFile)
	rplib.Shellexec("xz", "-0", imageFile)
}

var (
	optKernel    = "--kernel"
	optOs        = "--os"
	optGadget    = "--gadget"
	optBaseImage = "--output"
	optStore     = ""
	optDevice    = ""
	optChannel   = "--channel"
	optSize      = "--size"
	optDevmode   = ""
	optSsh       = ""
)

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
		BaseImage    string
		RecoveryType string
		RecoverySize string
		Release      string
		Store        string
		Device       string
		Channel      string
		Size         string
		OemHookDir   string
		OemLogDir    string
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

var configs YamlConfigs

func checkConfigs() bool {
	fmt.Printf("parseConfig ... \n")

	errCount := 0
	if configs.Project == "" {
		log.Println("Error: parse config.yaml failed, need to specify 'project' field")
		errCount++
	}

	if configs.Snaps.Kernel == "" {
		log.Println("Error: parse config.yaml failed, need to specify 'snaps -> kernel' field")
		errCount++
	}

	if configs.Snaps.Os == "" {
		log.Println("Error: parse config.yaml failed, need to specify 'snaps -> os' field")
		errCount++
	}

	if configs.Snaps.Gadget == "" {
		log.Println("Error: parse config.yaml failed, need to specify 'snaps -> gadget' field")
		errCount++
	}

	if configs.Configs.BaseImage == "" {
		log.Println("Error: parse config.yaml failed, need to specify 'configs -> baseimage' field")
		errCount++
	}

	if configs.Configs.RecoveryType == "" {
		log.Println("Error: parse config.yaml failed, need to specify 'configs -> recoverytype' field")
		errCount++
	}

	if configs.Configs.RecoverySize == "" {
		log.Println("Error: parse config.yaml failed, need to specify 'configs -> recoverysize' field")
		errCount++
	}

	if configs.Configs.Release == "" {
		log.Println("Error: parse config.yaml failed, need to specify 'configs -> release' field")
		errCount++
	}

	if configs.Configs.Channel == "" {
		log.Println("Error: parse config.yaml failed, need to specify 'configs -> channel' field")
		errCount++
	}

	if configs.Configs.Size == "" {
		log.Println("Error: parse config.yaml failed, need to specify 'configs -> size' field")
		errCount++
	}

	if configs.Udf.Binary == "" {
		log.Println("Error: parse config.yaml failed, need to specify 'udf -> binary' field")
		errCount++
	}

	if configs.Udf.Option == "" {
		log.Println("Error: parse config.yaml failed, need to specify 'udf -> option' field")
		errCount++
	}

	if configs.Recovery.FsLabel == "" {
		log.Println("Error: parse config.yaml failed, need to specify 'recovery -> filesystem-label' field")
		errCount++
	}

	if errCount > 0 {
		return true
	}

	if configs.Debug.Devmode {
		optDevmode = "--developer-mode"
	}

	if configs.Debug.Devmode {
		optSsh = "--enable-ssh"
	}

	if configs.Configs.Store != "" {
		optStore = "--store"
	}

	if configs.Configs.Device != "" {
		optDevice = "--device"
	}

	return false
}

func printConfigs() {
	fmt.Printf("Configs from yaml file\n")
	fmt.Println("-----------------------------------------------")
	fmt.Println("project: ", configs.Project)
	fmt.Println("kernel: ", configs.Snaps.Kernel)
	fmt.Println("os: ", configs.Snaps.Os)
	fmt.Println("gadget: ", configs.Snaps.Gadget)
	fmt.Println("baseimage: ", configs.Configs.BaseImage)
	fmt.Println("recoverytype: ", configs.Configs.RecoveryType)
	fmt.Println("recoverysize: ", configs.Configs.RecoverySize)
	fmt.Println("release: ", configs.Configs.Release)
	fmt.Println("store: ", configs.Configs.Store)
	fmt.Println("device: ", configs.Configs.Device)
	fmt.Println("channel: ", configs.Configs.Channel)
	fmt.Println("size: ", configs.Configs.Size)
	fmt.Println("oemhookdir: ", configs.Configs.OemHookDir)
	fmt.Println("oemlogdir: ", configs.Configs.OemLogDir)
	fmt.Println("udf binary: ", configs.Udf.Binary)
	fmt.Println("udf option: ", configs.Udf.Option)
	fmt.Println("devmode: ", configs.Debug.Devmode)
	fmt.Println("ssh: ", configs.Debug.Ssh)
	fmt.Println("xz: ", configs.Debug.Xz)
	fmt.Println("fslabel: ", configs.Recovery.FsLabel)
	fmt.Println("boot partition: ", configs.Recovery.BootPart)
	fmt.Println("system-boot partition: ", configs.Recovery.SystembootPart)
	fmt.Println("writable partition: ", configs.Recovery.WritablePart)
	fmt.Println("boot image: ", configs.Recovery.BootImage)
	fmt.Println("system-boot image: ", configs.Recovery.SystembootImage)
	fmt.Println("writable image: ", configs.Recovery.WritableImage)
	fmt.Println("-----------------------------------------------")
}

func loadYamlConfig(configFile string) (errBool bool) {
	fmt.Printf("Loading config file %s ...\n", configFile)
	filename, _ := filepath.Abs(configFile)
	yamlFile, err := ioutil.ReadFile(filename)

	if err != nil {
		fmt.Printf("Error: can not load %s\n", configFile)
		panic(err)
	}

	// Parse config.yaml and store in configs
	err = yaml.Unmarshal(yamlFile, &configs)
	if err != nil {
		fmt.Printf("Error: parse %s failed\n", configFile)
		panic(err)
	}

	// Check if there is any config missing
	errBool = checkConfigs()
	printConfigs()
	return errBool
}

func printUsage() {
	fmt.Println("")
	fmt.Println("ubuntu-recovery-image CONFIG_FOLDER")
	fmt.Println("CONFIG_FOLDER includes configurations to build the recovery image.")
	fmt.Println("")
}

func main() {
	// Print version
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	commitstampInt64, _ := strconv.ParseInt(commitstamp, 10, 64)
	log.Printf("Version: %v, Commit: %v, Commit date: %v\n", version, commit, time.Unix(commitstampInt64, 0).UTC())

	// Load the config folder from the first parameter
	var configFolder string

	args := os.Args
	numargs := len(args)

	if numargs == 2 {
		configFolder = args[1]
	} else {
		log.Fatal("Error: you need to provide config folder in the first parameter")
		printUsage()
	}

	// Parse config.yaml to configs struct
	errBool := loadYamlConfig(configFolder + "/config.yaml")
	if errBool {
		fmt.Println("Error: parse config.yaml failed")
		os.Exit(1)
	}

	log.Printf("[Setup project for %s]", configs.Project)

	// Create base image or recovery image or both according to 'recoverytype' field in config.yaml
	if configs.Configs.RecoveryType == "base" || configs.Configs.RecoveryType == "full" {
		createBaseImage()
		if configs.Configs.RecoveryType == "base" {
			log.Printf("[Create base image %s only]", configs.Configs.BaseImage)
			os.Exit(0)
		}
	} else if configs.Configs.RecoveryType == "recovery" {
		log.Printf("[Base image is %s]", configs.Configs.BaseImage)
	} else {
		fmt.Printf("Error: %q is not valid type.\n", configs.Configs.RecoveryType)
		os.Exit(2)
	}

	// Check if base image exists
	if _, err := os.Stat(configs.Configs.BaseImage); err != nil {
		fmt.Printf("Error: can not find base image: %s, please build base image first\n", configs.Configs.BaseImage)
		os.Exit(2)
	}

	// Create recovery image if 'recoverytype' field is 'recovery' or 'full' in config.yaml
	recoveryNR := "1"

	log.Printf("[start create recovery image with skipxz options: %s.\n]", configs.Debug.Xz)

	todayTime := time.Now()
	todayDate := fmt.Sprintf("%d%02d%02d", todayTime.Year(), todayTime.Month(), todayTime.Day())
	recoveryOutputFile := configs.Project + "-" + todayDate + "-0.img"

	createRecoveryImage(recoveryNR, recoveryOutputFile, configFolder)

	// Compress image to xz if 'xz' field is 'on' in config.yaml
	if configs.Debug.Xz {
		compressXZImage(recoveryOutputFile)
	}
}
