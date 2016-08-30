package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"syscall"
	"time"

	"gopkg.in/yaml.v2"
)

import rplib "github.com/Lyoncore/ubuntu-recovery-rplib"
import utils "github.com/Lyoncore/ubuntu-recovery-image/utils"

var version string
var commit string
var commitstamp string

// setupLoopDevice setup loop device for base image and recovery image.
func setupLoopDevice(recoveryOutputFile string, recoveryNR string, label string) (string, string) {
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
	err = syscall.Fallocate(int(outputfile.Fd()), 0, 0, basefilest.Size())
	rplib.Checkerr(err)

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
		"name", recoveryNR, label,
		"set", recoveryNR, "boot", "on",
		"print")

	return baseImageLoop, recoveryImageLoop
}

// find snap with input name
// input example:
//   - ubuntu-core_144.snap
//   - ubuntu-core
func findSnap(folder, input string) string {
	name := rplib.FindSnapName(input)

	// input is not a snap package file name
	// should be a package name (such as "ubuntu-core")
	if "" == name {
		name = input
	}
	log.Printf("findSnap: %s/%s_*.snap", folder, name)
	paths, err := filepath.Glob(fmt.Sprintf("%s/%s_*.snap", folder, name))
	rplib.Checkerr(err)
	if 1 != len(paths) {
		log.Println("paths:", paths)
		log.Panic("Should have one and only one specified snap")
	}
	path := paths[0]
	log.Printf("snap path:", path)
	return path
}

func setupInitrd(initrdImagePath string, tmpDir string) {
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

	log.Printf("[locate kernel snap and mount]")
	kernelSnapPath := findSnap(filepath.Join(tmpDir, "image/writable/system-data/var/lib/snapd/snaps/"), configs.Snaps.Kernel)

	rplib.Shellexec("mount", kernelSnapPath, kernelsnapTmpDir)
	defer syscall.Unmount(kernelsnapTmpDir, 0)

	log.Printf("[unxz initrd in kernel snap]")
	unxzInitrdCmd := fmt.Sprintf("unxz < %s/initrd.img | (cd %s; cpio -i )", kernelsnapTmpDir, initrdTmpDir)
	_ = rplib.Shellcmdoutput(unxzInitrdCmd)

	// overwrite initrd with initrd_local-include
	rplib.Shellexec("rsync", "-r", "initrd_local-includes/", initrdTmpDir)

	log.Printf("[recreate initrd]")
	_ = rplib.Shellcmdoutput(fmt.Sprintf("( cd %s; find | cpio --quiet -o -H newc ) | xz -c9 --check=crc32 > %s", initrdTmpDir, initrdImagePath))
}

func createBaseImage() {
	log.Println("Create base image, channel: %s", configs.Configs.Channel)

	if _, err := os.Stat(configs.Configs.BaseImage); err == nil {
		log.Println("The file %s exist, remove the file.", configs.Configs.BaseImage)
		err = os.Remove(configs.Configs.BaseImage)
		rplib.Checkerr(err)
	}

	configs.ExecuteUDF()
}

func createRecoveryImage(recoveryNR string, recoveryOutputFile string, buildstamp utils.BuildStamp) {
	var label string
	switch configs.Recovery.Type {
	case rplib.FIELD_TRANSITION:
		label = configs.Recovery.TransitionFsLabel
	default:
		label = configs.Recovery.FsLabel
	}

	// Setup loop devices
	baseImageLoop, recoveryImageLoop := setupLoopDevice(recoveryOutputFile, recoveryNR, label)
	// Delete loop devices
	defer rplib.Shellcmd(fmt.Sprintf("losetup -d /dev/%s", baseImageLoop))
	defer rplib.Shellcmd(fmt.Sprintf("losetup -d /dev/%s", recoveryImageLoop))
	log.Printf("[base image loop:%s, recovery image loop: %s created]\n", baseImageLoop, recoveryImageLoop)

	// Create device maps from partition tables
	log.Printf("[kpartx]")
	rplib.Shellexec("kpartx", "-avs", fmt.Sprintf("/dev/%s", baseImageLoop))
	rplib.Shellexec("kpartx", "-avs", fmt.Sprintf("/dev/%s", recoveryImageLoop))
	rplib.Shellexec("udevadm", "settle")
	// Delete device maps
	defer rplib.Shellexec("udevadm", "settle")
	defer rplib.Shellexec("kpartx", "-ds", fmt.Sprintf("/dev/%s", recoveryImageLoop))
	defer rplib.Shellexec("kpartx", "-ds", fmt.Sprintf("/dev/%s", baseImageLoop))

	// TODO: rewritten with launchpad/goget-ubuntu-touch/DiskImage image.Create
	log.Printf("[mkfs.fat]")
	rplib.Shellexec("mkfs.fat", "-F", "32", "-n", label, filepath.Join("/dev/mapper", fmt.Sprintf("%sp%s", recoveryImageLoop, recoveryNR)))

	tmpDir, err := ioutil.TempDir("", "")
	rplib.Checkerr(err)

	log.Printf("tmpDir:", tmpDir)
	defer os.RemoveAll(tmpDir) // clean up

	recoveryMapperDevice := fmt.Sprintf("/dev/mapper/%sp%s", recoveryImageLoop, recoveryNR)
	recoveryDir := filepath.Join(tmpDir, "device", configs.Recovery.FsLabel)
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
		if match, _ := regexp.MatchString("system-boot|writable", label); match {
			log.Printf("matched")
			baseDir := fmt.Sprintf("%s/image/%s", tmpDir, label)
			err := os.MkdirAll(baseDir, 0755)
			rplib.Checkerr(err)
			defer os.RemoveAll(baseDir) // clean up

			log.Printf("[mount device %s on base image dir %s]", part, label)
			fstype := rplib.Shellexecoutput("blkid", part, "-o", "value", "-s", "TYPE")
			log.Println("fstype:", fstype)
			err = syscall.Mount(part, baseDir, fstype, 0, "")
			rplib.Checkerr(err)

			defer syscall.Unmount(baseDir, 0)
		}
	}

	// add buildstamp
	log.Printf("save buildstamp")
	d, err := yaml.Marshal(&buildstamp)
	rplib.Checkerr(err)
	err = ioutil.WriteFile(filepath.Join(recoveryDir, utils.BuildStampFile), d, 0644)
	rplib.Checkerr(err)

	log.Printf("[deploy default efi bootdir]")

	// add efi/
	rplib.Shellexec("cp", "-ar", fmt.Sprintf("%s/image/system-boot/efi", tmpDir), recoveryDir)

	// edit efi/ubuntu/grub/grubenv
	err = os.Remove(filepath.Join(recoveryDir, "efi/ubuntu/grub/grubenv"))
	rplib.Checkerr(err)
	log.Printf("[create grubenv for switching between core and recovery system]")
	rplib.Shellexec("grub-editenv", filepath.Join(recoveryDir, "efi/ubuntu/grub/grubenv"), "create")
	rplib.Shellexec("grub-editenv", filepath.Join(recoveryDir, "efi/ubuntu/grub/grubenv"), "set", "firstfactoryrestore=no")
	rplib.Shellexec("grub-editenv", filepath.Join(recoveryDir, "efi/ubuntu/grub/grubenv"), "set", "recoverylabel="+label)
	rplib.Shellexec("grub-editenv", filepath.Join(recoveryDir, "efi/ubuntu/grub/grubenv"), "set", "recoverytype="+configs.Recovery.Type)

	os.Mkdir(fmt.Sprintf("%s/oemlog", recoveryDir), 0755)

	// add recovery/factory/
	err = os.MkdirAll(filepath.Join(recoveryDir, "recovery/factory"), 0755)
	rplib.Checkerr(err)

	// add recovery/config.yaml
	log.Printf("[add config.yaml]")
	rplib.Shellexec("cp", "-f", "config.yaml", filepath.Join(recoveryDir, "recovery"))

	if configs.Configs.OemPreinstHookDir != "" {
		log.Printf("[Create oem specific pre-install hook directory]")
		err = os.Mkdir(filepath.Join(recoveryDir, "recovery/factory", configs.Configs.OemPreinstHookDir), 0755)
		rplib.Checkerr(err)
	}

	if configs.Configs.OemPostinstHookDir != "" {
		log.Printf("[Create oem specific post-install hook directory]")
		err = os.Mkdir(filepath.Join(recoveryDir, "recovery/factory", configs.Configs.OemPostinstHookDir), 0755)
		rplib.Checkerr(err)
	}

	// add recovery/factory/system-boot.tar.xz
	// add recovery/factory/writable.tar.xz
	if configs.Recovery.SystembootImage != "" && configs.Recovery.WritableImage != "" {
		log.Printf("Copy user provided system-boot (%s) and writable (%s) images to %s/recovery/factory directory\n",
			configs.Recovery.SystembootImage, configs.Recovery.WritableImage, recoveryDir)

		rplib.Shellexec("cp", configs.Recovery.SystembootImage, filepath.Join(recoveryDir, "recovery/factory/"))
		rplib.Shellexec("cp", configs.Recovery.WritableImage, filepath.Join(recoveryDir, "recovery/factory/"))
	} else {
		log.Printf("add system-data and writable tarball from base image")

		workingDir, err := os.Getwd()
		rplib.Checkerr(err)

		err = os.Chdir(fmt.Sprintf("%s/image/system-boot/", tmpDir))
		rplib.Checkerr(err)
		rplib.Shellexec("tar", "--xattrs", "-Jcpf", filepath.Join(recoveryDir, "recovery/factory/system-boot.tar.xz"), ".")

		err = os.Chdir(fmt.Sprintf("%s/image/writable/", tmpDir))
		rplib.Checkerr(err)
		rplib.Shellexec("tar", "--xattrs", "-Jcpf", filepath.Join(recoveryDir, "recovery/factory/writable.tar.xz"), ".")

		err = os.Chdir(workingDir)
		rplib.Checkerr(err)
	}

	// add kernel.snap
	kernelSnap := findSnap(filepath.Join(tmpDir, "image/writable/system-data/var/lib/snapd/snaps/"), configs.Snaps.Kernel)
	rplib.Shellexec("cp", "-f", kernelSnap, filepath.Join(recoveryDir, "kernel.snap"))
	// add gadget.snap
	gadgetSnap := findSnap(filepath.Join(tmpDir, "image/writable/system-data/var/lib/snapd/snaps/"), configs.Snaps.Gadget)
	rplib.Shellexec("cp", "-f", gadgetSnap, filepath.Join(recoveryDir, "gadget.snap"))
	// add os.snap
	osSnap := findSnap(filepath.Join(tmpDir, "image/writable/system-data/var/lib/snapd/snaps/"), configs.Snaps.Os)
	rplib.Shellexec("cp", "-f", osSnap, filepath.Join(recoveryDir, "os.snap"))

	// add initrd.img
	log.Printf("[setup initrd.img]")
	initrdImagePath := fmt.Sprintf("%s/initrd.img", recoveryDir)
	setupInitrd(initrdImagePath, tmpDir)

	// overwrite with local-includes in configuration
	log.Printf("[add local-includes]")
	rplib.Shellexec("rsync", "-r", "local-includes/", recoveryDir)
}

func compressXZImage(imageFile string) {
	log.Printf("[compress image: %s.xz]", imageFile)
	rplib.Shellexec("xz", "-0", imageFile)
}

func printUsage() {
	log.Println("ubuntu-recovery-image")
	log.Println("[execute ubuntu-recovery-image in config folder]")
	log.Println("")
}

var configs rplib.ConfigRecovery

func main() {
	// Print version
	const configFile = "config.yaml"
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	if "" == version {
		version = utils.Version
	}

	commitstampInt64, _ := strconv.ParseInt(commitstamp, 10, 64)
	var buildstamp = utils.BuildStamp{
		BuildDate: time.Now().UTC(),
		BuildTool: utils.ProjectInfo{
			Version:     version,
			Commit:      commit,
			CommitStamp: time.Unix(commitstampInt64, 0).UTC(),
		},
		BuildConfig: utils.ProjectInfo{
			Version:     utils.ReadVersionFromPackageJson(),
			Commit:      utils.GetGitSha(),
			CommitStamp: time.Unix(utils.CommitStamp(), 0).UTC(),
		},
	}
	log.Printf("Version: %v, Commit: %v, Commit date: %v\n", version, commit, time.Unix(commitstampInt64, 0).UTC())

	// Load configuration
	err := configs.Load(configFile)
	rplib.Checkerr(err)

	log.Println(configs)

	log.Printf("[Setup project for %s]", configs.Project)

	// Create base image or recovery image or both according to 'recoverytype' field
	switch configs.Configs.RecoveryType {
	case "base", "full":
		createBaseImage()
		if configs.Configs.RecoveryType == "base" {
			log.Println("[Create base image %s only]", configs.Configs.BaseImage)
			os.Exit(0)
		}
	case "recovery":
		log.Println("[Base image is %s]", configs.Configs.BaseImage)
	default:
		log.Println("Error: %q is not valid type.", configs.Configs.RecoveryType)
		os.Exit(2)
	}

	// Check if base image exists
	if _, err := os.Stat(configs.Configs.BaseImage); err != nil {
		log.Println("Error: can not find base image: %s, please build base image first", configs.Configs.BaseImage)
		os.Exit(2)
	}

	// Create recovery image if 'recoverytype' field is 'recovery' or 'full'
	recoveryNR := "1"

	log.Printf("[start create recovery image with skipxz options: %s.\n]", configs.Debug.Xz)

	todayTime := time.Now()
	todayDate := fmt.Sprintf("%d%02d%02d", todayTime.Year(), todayTime.Month(), todayTime.Day())
	defaultOutputFilename := configs.Project + "-" + todayDate + "-0.img"
	recoveryOutputFile := flag.String("o", defaultOutputFilename, "Name of the recovery image file to create")
	flag.Parse()

	createRecoveryImage(recoveryNR, *recoveryOutputFile, buildstamp)

	// Compress image to xz if 'xz' field is 'on'
	if configs.Debug.Xz {
		compressXZImage(*recoveryOutputFile)
	}
}
