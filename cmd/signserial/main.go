package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"path/filepath"
	"strconv"
	"time"

	"github.com/snapcore/snapd/asserts"
	"gopkg.in/yaml.v2"
)

import rplib "github.com/Lyoncore/ubuntu-recovery-rplib"
import utils "github.com/Lyoncore/ubuntu-recovery-image/utils"

var version string
var commit string
var commitstamp string
var build_date string

type YamlConfig struct {
	ModelAssertionFile string `yaml:"ModelAssertionFile"`
	TargetFolder       string `yaml:"TargetFolder"`
	Apikey             string `yaml:"ApiKey"`
	SignServer         string `yaml:"SignServer"`
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	if "" == version {
		version = utils.Version
	}
	commitstampInt64, _ := strconv.ParseInt(commitstamp, 10, 64)
	log.Printf("Version: %v, Commit: %v, Build date: %v\n", version, commit, time.Unix(commitstampInt64, 0).UTC())
	fmt.Println("You could feed entropy using rngd when testing. e.g.:")
	fmt.Println("rngd -r /dev/urandom")

	config := YamlConfig{}
	flag.Parse()

	if 1 != len(flag.Args()) {
		log.Println(`config file example:
ModelAssertionFile: modelAssertionMock.txt
TargetFolder: /writable/recovery
SignServer: http://localhost:8080/1.0/sign
ApiKey: U2VyaWFsIFZhdWx0Cg
`)
		log.Fatal("You need to provide a config file")
	}

	filename := flag.Arg(0)
	yamlFile, err := ioutil.ReadFile(filename)
	rplib.Checkerr(err)
	err = yaml.Unmarshal(yamlFile, &config)
	rplib.Checkerr(err)
	log.Printf("config: %+v\n", config)

	if "" == config.TargetFolder {
		log.Fatal("You need to provide target folder to store serial assertion")
	}

	fileContent, err := ioutil.ReadFile(config.ModelAssertionFile)
	rplib.Checkerr(err)
	modelAssertion, err := asserts.Decode(fileContent)
	rplib.Checkerr(err)

	if "" != config.SignServer {
		err = rplib.SignSerial(modelAssertion, config.TargetFolder, config.SignServer, config.Apikey)
		rplib.Checkerr(err)
	} else {
		content, err := rplib.SerialAssertionGen(modelAssertion, config.TargetFolder)
		rplib.Checkerr(err)
		err = ioutil.WriteFile(filepath.Join(config.TargetFolder, rplib.SerialUnsigned), []byte(content), 0600)
		rplib.Checkerr(err)
	}
}
