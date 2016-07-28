package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"strconv"
	"time"

	"github.com/snapcore/snapd/asserts"
)

import rplib "github.com/Lyoncore/ubuntu-recovery-rplib"
import utils "github.com/Lyoncore/ubuntu-recovery-image/utils"

var version string
var commit string
var commitstamp string
var build_date string

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	if "" == version {
		version = utils.Version
	}
	commitstampInt64, _ := strconv.ParseInt(commitstamp, 10, 64)
	log.Printf("Version: %v, Commit: %v, Build date: %v\n", version, commit, time.Unix(commitstampInt64, 0).UTC())
	fmt.Println("You could feed entropy using rngd when testing. e.g.:")
	fmt.Println("rngd -r /dev/urandom")

	modelAssertionFile := flag.String("modelAssert", "", "file of model assertion")
	targetFolder := flag.String("target", "", "target folder to store serial assertion")
	apikey := flag.String("apikey", "", "apikey of signing server")
	signServer := flag.String("signServer", "", "url of signing server")
	flag.Parse()

	if "" == *targetFolder {
		log.Fatal("You need to provide target folder to store serial assertion")
	}

	fileContent, err := ioutil.ReadFile(*modelAssertionFile)
	rplib.Checkerr(err)
	modelAssertion, err := asserts.Decode(fileContent)
	rplib.Checkerr(err)

	if "" != *signServer {
		err = rplib.SignSerial(modelAssertion, *targetFolder, *signServer, *apikey)
		rplib.Checkerr(err)
	} else {
		content, err := rplib.SerialAssertionGen(modelAssertion, *targetFolder)
		rplib.Checkerr(err)
		err = ioutil.WriteFile(*targetFolder+"/"+rplib.SerialUnsigned, []byte(content), 0600)
		rplib.Checkerr(err)
	}
}
