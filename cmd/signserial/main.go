package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/ubuntu-core/identity-vault/service"
	"gopkg.in/yaml.v2"
)

import rplib "github.com/Lyoncore/ubuntu-recovery-rplib"
import utils "github.com/Lyoncore/ubuntu-recovery-image/utils"

var version string
var commit string
var commitstamp string
var build_date string

type YamlConfig struct {
	ModelAssertionFile  string `yaml:"ModelAssertionFile"`
	DeviceKeyFile       string `yaml:"DeviceKeyFile"`
	Apikey              string `yaml:"ApiKey"`
	SignServer          string `yaml:"SignServer"`
	SerialRequestFile   string `yaml:"SerialRequestFile"`
	SerialAssertionFile string `yaml:"SerialAssertionFile"`
}

func GetNonce(vaultServer string, apikey string) (string, error) {
	// get nonce
	body := bytes.NewBuffer([]byte(""))
	vaultServer = strings.TrimRight(vaultServer, "/")
	vaultServer = vaultServer + "/request-id"
	log.Println("send request to:", vaultServer)
	req, err := http.NewRequest("POST", vaultServer, body)
	if err != nil {
		return "", err
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("api-key", apikey)

	client := &http.Client{}
	response, err := client.Do(req)
	if nil != err {
		return "", err
	}
	defer response.Body.Close()

	returnBody, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return "", err
	}
	var nonceResponse service.RequestIDResponse
	err = json.Unmarshal(returnBody, &nonceResponse)
	if err != nil {
		log.Println("returnBody:", string(returnBody))
		return "", err
	}
	return nonceResponse.RequestID, nil
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
		config = YamlConfig{
			ModelAssertionFile:  "modelAssertionMock.txt",
			DeviceKeyFile:       "TestDeviceKey.asc",
			Apikey:              "U2VyaWFsIFZhdWx0Cg",
			SignServer:          "http://localhost:8080/1.0/sign",
			SerialRequestFile:   "serial.request",
			SerialAssertionFile: "serial.assertion",
		}
		out, err := yaml.Marshal(config)
		if err != nil {
			log.Println(err)
		}
		log.Println("config file example:")
		log.Println(string(out))
		log.Fatal("You need to provide a config file")
	}

	filename := flag.Arg(0)
	yamlFile, err := ioutil.ReadFile(filename)
	rplib.Checkerr(err)
	err = yaml.Unmarshal(yamlFile, &config)
	rplib.Checkerr(err)
	log.Printf("config: %+v\n", config)

	// load device key
	var devicePrivKey asserts.PrivateKey
	armored, err := ioutil.ReadFile(config.DeviceKeyFile)
	if err == nil {
		devicePrivKey, _ = assertstest.ReadPrivKey(string(armored))
		log.Println("device key have been read:", devicePrivKey.PublicKey().ID())
	}
	if devicePrivKey == nil {
		// key file not loaded. generate new keypair
		devicePrivKey, armored, err = rplib.GenerateKey(4096)
		rplib.Checkerr(err)
		log.Println("new generated Public key id of devicePrivKey: ", devicePrivKey.PublicKey().ID())

		// export armored private key
		ioutil.WriteFile(config.DeviceKeyFile, armored, 0600)
	}

	fileContent, err := ioutil.ReadFile(config.ModelAssertionFile)
	rplib.Checkerr(err)
	modelAssertion, err := asserts.Decode(fileContent)
	rplib.Checkerr(err)

	// generate serial-request
	nonce, err := GetNonce(config.SignServer, config.Apikey)
	rplib.Checkerr(err)
	log.Println("nonce:", nonce)
	serialRequest, err := rplib.NewSerialRequest(modelAssertion, devicePrivKey, "A1234567", "1", nonce)
	rplib.Checkerr(err)
	err = ioutil.WriteFile(config.SerialRequestFile, asserts.Encode(serialRequest), 0600)
	rplib.Checkerr(err)

	// send sign request
	if config.SignServer != "" {
		ret, err := rplib.SendSerialRequest(serialRequest, config.SignServer, config.Apikey)
		rplib.Checkerr(err)
		err = ioutil.WriteFile(config.SerialAssertionFile, ret, 0600)
		rplib.Checkerr(err)
	}
}
