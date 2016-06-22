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

var version string
var commit string
var commitstamp string
var build_date string

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	commitstampInt64, _ := strconv.ParseInt(commitstamp, 10, 64)
	log.Printf("Version: %v, Commit: %v, Build date: %v\n", version, commit, time.Unix(commitstampInt64, 0).UTC())
	fmt.Println("You could feed entropy using rngd when testing. e.g.:")
	fmt.Println("rngd -r /dev/urandom")

	modelAssertionFile := flag.String("modelAssert", "", "file of model assertion")
	targetFolder := flag.String("target", "", "target folder to store serial assertion")
	signServer := flag.String("signServer", "", "url of signing server")
	flag.Parse()

	if "" == *targetFolder {
		log.Fatal("You need to provide target folder to store serial assertion")
	}

	fileContent, err := ioutil.ReadFile(*modelAssertionFile)
	rplib.Checkerr(err)
	modelAssertion, err := asserts.Decode(fileContent)
	rplib.Checkerr(err)

	rplib.SignSerial(modelAssertion, *targetFolder, *signServer)
}
