package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v2"

	rplib "github.com/Lyoncore/ubuntu-recovery-rplib"
	configdirs "github.com/Lyoncore/ubuntu-recovery-rplib/dirs/configdir"
	recoverydirs "github.com/Lyoncore/ubuntu-recovery-rplib/dirs/recovery"

	utils "github.com/Lyoncore/ubuntu-recovery-image/utils"
)

var version string
var commit string
var commitstamp string

// MaxInt64 returns the larger of a and b.
func MaxInt64(a, b int64) int64 {
	if a > b {
		return a
	}

	return b
}

// setupLoopDevice setup loop device for base image and recovery image.
func setupLoopDevice(recoveryOutputFile string, recoveryNR string, label string) (string, string) {
	log.Printf("[SETUP_LOOPDEVICE]")

	basefile, err := os.Open(configs.Configs.BaseImage)
	rplib.Checkerr(err)
	defer basefile.Close()
	basefilest, err := basefile.Stat()
	rplib.Checkerr(err)

	outputfile, err := os.Create(recoveryOutputFile)
	rplib.Checkerr(err)
	defer outputfile.Close()

	recoverySize, err := strconv.Atoi(configs.Configs.RecoverySize)
	rplib.Checkerr(err)

	// TODO: calculate recovery partition size dynamically.
	// add 20 Megabytes for filesystem meta data
	// image size should be larger than or equal to base image. or the gpt table copy would failed
	imageSize := MaxInt64(int64(recoverySize+20)*1024*1024, basefilest.Size())
	err = syscall.Fallocate(int(outputfile.Fd()), 0, 0, imageSize)
	rplib.Checkerr(err)

	//copy partition table
	log.Printf("Copy partitition table")
	rplib.Shellcmd(fmt.Sprintf("sfdisk -d %s | sfdisk %s", configs.Configs.BaseImage, recoveryOutputFile))
	log.Println("[recover the backup GPT entry]")
	rplib.Shellexec("sgdisk", recoveryOutputFile, "--randomize-guids", "--move-second-header")

	var last_end int
	const PARTITION = "/tmp/partition.txt"
	rplib.Shellcmd(fmt.Sprintf("parted -ms %s unit B print | sed -n '1,2!p' > %s", configs.Configs.BaseImage, PARTITION))
	//dd bootloader from base image
	var f *(os.File)
	f, err = os.Open(PARTITION)
	rplib.Checkerr(err)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Split(line, ":")
		nr, err := strconv.Atoi(fields[0])
		rplib.Checkerr(err)
		begin := strings.TrimRight(fields[1], "B")
		end, err := strconv.Atoi(strings.TrimRight(fields[2], "B"))
		rplib.Checkerr(err)
		size := strings.TrimRight(fields[3], "B")
		log.Println("nr: ", nr)
		log.Println("begin: ", begin)
		log.Println("end: ", end)
		log.Println("size: ", size)

		//dd data before partition
		if nr == 1 {
			log.Printf("Copy raw data")
			begin_nr, err := strconv.Atoi(begin)
			rplib.Checkerr(err)
			if configs.Configs.Bootloader == "gpt" {
				rplib.DD(configs.Configs.BaseImage, recoveryOutputFile, "bs=512", "skip=34", "seek=34", fmt.Sprintf("count=%s", (begin_nr/512)-34), "conv=notrunc")
			} else if configs.Configs.Bootloader == "mbr" {
				rplib.DD(configs.Configs.BaseImage, recoveryOutputFile, "bs=512", "skip=1", "seek=1", fmt.Sprintf("count=%s", (begin_nr/512)-1), "conv=notrunc")
			}
		}

		if recovery_nr, err := strconv.Atoi(recoveryNR); err == nil {
			if nr < recovery_nr {
				rplib.DD(configs.Configs.BaseImage, recoveryOutputFile, "bs=1", fmt.Sprintf("skip=%s", begin), fmt.Sprintf("seek=%s", begin), fmt.Sprintf("count=%s", size), "conv=notrunc")
				last_end = end
			} else { //remove paritions which recovery and after partitions
				rplib.Shellexec("parted", "-ms", recoveryOutputFile, "rm", fmt.Sprintf("%v", nr))
			}
		}
	}

	nr, err := strconv.Atoi(recoveryNR)
	rplib.Checkerr(err)
	var recoveryBegin int

	if nr == 1 {
		recoveryBegin = 4194304 //4MiB
	} else {
		recoveryBegin = last_end + 1
	}
	recoveryEnd := recoveryBegin + (recoverySize * 1024 * 1024)

	if configs.Configs.PartitionType == "gpt" {
		rplib.Shellexec("parted", "-ms", "-a", "optimal", recoveryOutputFile,
			"unit", "B",
			"mkpart", "primary", "fat32", fmt.Sprintf("%d", recoveryBegin), fmt.Sprintf("%d", recoveryEnd),
			"name", recoveryNR, label,
			"print")
	} else if configs.Configs.PartitionType == "mbr" {
		rplib.Shellexec("parted", "-ms", "-a", "optimal", recoveryOutputFile,
			"unit", "B",
			"mkpart", "primary", "fat32", fmt.Sprintf("%d", recoveryBegin), fmt.Sprintf("%d", recoveryEnd),
			"print")
	}

	//mark bootable if recovery in first partition
	if nr == 1 {
		rplib.Shellexec("parted", "-ms", recoveryOutputFile,
			"set", recoveryNR, "boot", "on")
	}

	log.Printf("[setup a loopback device for recovery image %s]", recoveryOutputFile)
	recoveryImageLoop := rplib.Shellcmdoutput(fmt.Sprintf("losetup --partscan --find --show %s | xargs basename", recoveryOutputFile))

	log.Printf("[setup a readonly loopback device for base image]")
	baseImageLoop := rplib.Shellcmdoutput(fmt.Sprintf("losetup -r --partscan --find --show %s | xargs basename", configs.Configs.BaseImage))

	log.Printf("[create %s partition on %s]", recoveryOutputFile, recoveryImageLoop)

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
	initrdImg := filepath.Join(kernelsnapTmpDir, "initrd.img")
	content, err := ioutil.ReadFile(initrdImg)
	rplib.Checkerr(err)
	filetype := http.DetectContentType(content)
	log.Println("filetype:", filetype)
	var extractCmd string
	switch filetype {
	case "application/x-gzip":
		extractCmd = "gunzip"
	case "application/octet-stream":
		extractCmd = "unxz"
	default:
		panic("Uknown file type")
	}
	extractInitrdCmd := fmt.Sprintf("%s < %s/initrd.img | (cd %s; cpio -i )", extractCmd, kernelsnapTmpDir, initrdTmpDir)
	_ = rplib.Shellcmdoutput(extractInitrdCmd)

	// overwrite initrd with initrd_local-include
	rplib.Shellexec("rsync", "-r", "--exclude", ".gitkeep", "initrd_local-includes/", initrdTmpDir)

	log.Printf("[recreate initrd]")
	switch filetype {
	case "application/x-gzip":
		_ = rplib.Shellcmdoutput(fmt.Sprintf("( cd %s; find | cpio --quiet -o -H newc ) | gzip -9 > %s", initrdTmpDir, initrdImagePath))
	case "application/octet-stream":
		_ = rplib.Shellcmdoutput(fmt.Sprintf("( cd %s; find | cpio --quiet -o -H newc ) | xz -c9 --check=crc32 > %s", initrdTmpDir, initrdImagePath))
	default:
		panic("Uknown file type")
	}
}

func createRecoveryImage(recoveryNR string, recoveryOutputFile string, buildstamp utils.BuildStamp) {
	var label string
	switch configs.Recovery.ImageType {
	case rplib.HEADLESS_INSTALLER:
		label = configs.Recovery.InstallerFsLabel
	default:
		label = configs.Recovery.FsLabel
	}

	// Setup loop devices
	baseImageLoop, recoveryImageLoop := setupLoopDevice(recoveryOutputFile, recoveryNR, label)
	// Delete loop devices
	defer rplib.Shellcmd(fmt.Sprintf("losetup -d /dev/%s", baseImageLoop))
	defer rplib.Shellcmd(fmt.Sprintf("losetup -d /dev/%s", recoveryImageLoop))
	log.Printf("[base image loop:%s, recovery image loop: %s created]\n", baseImageLoop, recoveryImageLoop)

	// TODO: rewritten with launchpad/goget-ubuntu-touch/DiskImage image.Create
	log.Printf("[mkfs.fat]")
	rplib.Shellexec("mkfs.fat", "-F", "32", "-n", label, filepath.Join("/dev/", fmt.Sprintf("%sp%s", recoveryImageLoop, recoveryNR)))

	tmpDir, err := ioutil.TempDir("", "")
	rplib.Checkerr(err)

	log.Printf("tmpDir:", tmpDir)
	defer os.RemoveAll(tmpDir) // clean up

	recoveryMapperDevice := fmt.Sprintf("/dev/%sp%s", recoveryImageLoop, recoveryNR)
	recoveryDir := filepath.Join(tmpDir, "device", label)
	log.Printf("[mkdir %s]", recoveryDir)
	recoverydirs.SetRootDir(recoveryDir)

	err = os.MkdirAll(recoveryDir, 0755)
	rplib.Checkerr(err)

	log.Printf("[mount device %s on recovery dir %s]", recoveryMapperDevice, recoveryDir)
	err = syscall.Mount(recoveryMapperDevice, recoveryDir, "vfat", 0, "")
	rplib.Checkerr(err)
	defer syscall.Unmount(recoveryDir, 0)

	baseMapperDeviceGlobName := fmt.Sprintf("/dev/%s*", baseImageLoop)
	baseMapperDeviceArray, err := filepath.Glob(baseMapperDeviceGlobName)
	rplib.Checkerr(err)

	// mount the base image
	for _, part := range baseMapperDeviceArray {
		fsType := rplib.Shellexecoutput("blkid", part, "-o", "value", "-s", "TYPE")
		fsType = strings.TrimSpace(fsType)
		log.Println("fsType:", fsType)
		var partition string
		switch fsType {
		case "vfat":
			partition = "system-boot"
		case "ext4":
			partition = "writable"
		default:
			continue
		}
		baseDir := filepath.Join(tmpDir, "image", partition)
		err := os.MkdirAll(baseDir, 0755)
		rplib.Checkerr(err)
		defer os.RemoveAll(baseDir) // clean up

		log.Printf("[mount device %s on base image dir %s , baseDir: %s]", part, partition, baseDir)
		rplib.Shellexec("mount", "-o", "ro", part, baseDir)
		//err = syscall.Mount(part, baseDir, fsType, 0, "")
		//rplib.Checkerr(err)
		defer syscall.Unmount(baseDir, 0)

	}

	// add buildstamp
	log.Printf("save buildstamp")
	d, err := yaml.Marshal(&buildstamp)
	rplib.Checkerr(err)
	err = ioutil.WriteFile(filepath.Join(recoveryDir, utils.BuildStampFile), d, 0644)
	rplib.Checkerr(err)

	log.Printf("[deploy default efi bootdir]")

	if configs.Configs.Bootloader == "grub" {
		// add efi/
		rplib.Shellexec("cp", "-ar", fmt.Sprintf("%s/image/system-boot/efi", tmpDir), recoveryDir)

		// edit efi/ubuntu/grub/grubenv
		err = os.Remove(filepath.Join(recoveryDir, "efi/ubuntu/grubenv"))
		rplib.Checkerr(err)
		log.Printf("[create grubenv for switching between core and recovery system]")
		rplib.Shellexec("grub-editenv", filepath.Join(recoveryDir, "efi/ubuntu/grubenv"), "create")
		rplib.Shellexec("grub-editenv", filepath.Join(recoveryDir, "efi/ubuntu/grubenv"), "set", "firstfactoryrestore=no")
		rplib.Shellexec("grub-editenv", filepath.Join(recoveryDir, "efi/ubuntu/grubenv"), "set", "recoverylabel="+configs.Recovery.FsLabel)
		rplib.Shellexec("grub-editenv", filepath.Join(recoveryDir, "efi/ubuntu/grubenv"), "set", "recoverytype="+configs.Recovery.Type)
		if configs.Recovery.InstallerFsLabel != "" {
			rplib.Shellexec("grub-editenv", filepath.Join(recoveryDir, "efi/ubuntu/grubenv"), "set", "installerfslabel="+configs.Recovery.InstallerFsLabel)
		}
	} else if configs.Configs.Bootloader == "u-boot" {
		rplib.Shellexec("rsync", "-aAX", "--exclude=*.snap", fmt.Sprintf("%s/image/system-boot/", tmpDir), recoveryDir)
		log.Printf("[create uEnv.txt]")
		rplib.Shellexec("cp", "-f", "local-includes/uEnv.txt", fmt.Sprintf("%s/uEnv.txt", recoveryDir))
	}

	// add recovery/factory/
	err = os.MkdirAll(filepath.Join(recoveryDir, "recovery/factory"), 0755)
	rplib.Checkerr(err)

	// add recovery/config.yaml
	log.Printf("[add config.yaml]")
	rplib.Shellexec("cp", "-f", "config.yaml", filepath.Join(recoveryDir, "recovery"))

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
		rplib.Shellexec("tar", "--xattrs", "-I", "pxz -0 -T 4", "-cpf", filepath.Join(recoveryDir, "recovery/factory/system-boot.tar.xz"), ".")

		err = os.Chdir(fmt.Sprintf("%s/image/writable/", tmpDir))
		rplib.Checkerr(err)
		rplib.Shellexec("tar", "--xattrs", "-I", "pxz -0 -T 4", "-cpf", filepath.Join(recoveryDir, "recovery/factory/writable.tar.xz"), ".")

		err = os.Chdir(workingDir)
		rplib.Checkerr(err)
	}

	// add /recovery/writable_local-include.squashfs
	rplib.Shellexec("mksquashfs", configdirs.WritableLocalIncludeDir, recoverydirs.WritableLocalIncludeSquashfs, "-all-root")

	// add kernel.snap
	kernelSnap := findSnap(filepath.Join(tmpDir, "image/writable/system-data/var/lib/snapd/seed/snaps/"), configs.Snaps.Kernel)
	rplib.Shellexec("cp", "-f", kernelSnap, filepath.Join(recoveryDir, "kernel.snap"))
	// add gadget.snap
	gadgetSnap := findSnap(filepath.Join(tmpDir, "image/writable/system-data/var/lib/snapd/seed/snaps/"), configs.Snaps.Gadget)
	rplib.Shellexec("cp", "-f", gadgetSnap, filepath.Join(recoveryDir, "gadget.snap"))
	// add os.snap
	osSnap := findSnap(filepath.Join(tmpDir, "image/writable/system-data/var/lib/snapd/seed/snaps/"), configs.Snaps.Os)
	rplib.Shellexec("cp", "-f", osSnap, filepath.Join(recoveryDir, "os.snap"))

	//Update uEnv.txt for os.snap/kernel.snap
	if configs.Configs.Bootloader == "u-boot" {
		log.Printf("[Set os/kernel snap in uEnv.txt]")
		f, err := os.OpenFile(fmt.Sprintf("%s/uEnv.txt", recoveryDir), os.O_APPEND|os.O_WRONLY, 0644)
		rplib.Checkerr(err)
		defer f.Close()
		_, err = f.WriteString(fmt.Sprintf("snap_core=%s\n", path.Base(osSnap)))
		_, err = f.WriteString(fmt.Sprintf("snap_kernel=%s\n", path.Base(kernelSnap)))
		rplib.Checkerr(err)
	}

	// add initrd.img
	log.Printf("[setup initrd.img]")
	initrdImagePath := fmt.Sprintf("%s/initrd.img", recoveryDir)
	setupInitrd(initrdImagePath, tmpDir)

	// overwrite with local-includes in configuration
	log.Printf("[add local-includes]")
	rplib.Shellexec("rsync", "-r", "--exclude", ".gitkeep", "local-includes/", recoveryDir)
}

func compressXZImage(imageFile string) {
	log.Printf("[compress image: %s.xz]", imageFile)
	rplib.Shellexec("pxz", "-0", "-T", "4", imageFile)
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

	log.Println("[Base image is %s]", configs.Configs.BaseImage)

	// Check if base image exists
	if _, err := os.Stat(configs.Configs.BaseImage); err != nil {
		log.Println("Error: can not find base image: %s, please build base image first", configs.Configs.BaseImage)
		os.Exit(2)
	}

	var recoveryNR string
	// Create recovery image if 'recoverytype' field is 'recovery' or 'full'
	if configs.Configs.Bootloader == "grub" {
		recoveryNR = "1"
	} else if configs.Configs.Bootloader == "u-boot" {
		//u-boot must put uboot.env in system-boot and partition need in fixing location
		//So, let recovry partition location in next to system-boot (the orignal writable)
		const PARTITON = "/tmp/partition"
		Loop := rplib.Shellcmdoutput(fmt.Sprintf("losetup --partscan --find --show %s | xargs basename", configs.Configs.BaseImage))
		recovery_part := rplib.Findfs("LABEL=writable") //new recovery partition locate in writable
		recoveryNR = strings.Trim(recovery_part, fmt.Sprintf("/dev/%sp", Loop))
		defer rplib.Shellcmdoutput(fmt.Sprintf("losetup -d %s", configs.Configs.BaseImage))
	}

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
