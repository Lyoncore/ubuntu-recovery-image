package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"strconv"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/systestkeys"
	"github.com/ubuntu-core/identity-vault/service"
	"gopkg.in/yaml.v2"
)

import rplib "github.com/Lyoncore/ubuntu-recovery-rplib"
import utils "github.com/Lyoncore/ubuntu-recovery-image/utils"

var version string
var commit string
var commitstamp string
var build_date string

type Output struct {
	KeypairJsonFile         string
	ModelJsonFile           string
	AccountAssertionFile    string
	AccountKeyAssertionFile string
	ModelAssertionFile      string
	SerialAssertionFile     string
}

type ConfigSettings struct {
	DBStorePath   string
	TestKeyFile   string
	DeviceKeyFile string
	AccountID     string
	Model         string
	Serial        string
	Nonce         string

	Output Output
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

	config := ConfigSettings{}
	flag.Parse()
	if 1 != len(flag.Args()) {
		config = ConfigSettings{
			DBStorePath:   "./Keystore/",
			TestKeyFile:   "TestKey.asc",
			DeviceKeyFile: "TestDeviceKey.asc",
			AccountID:     "System",
			Model:         "Router 3400",
			Serial:        "A1228ML",
			Nonce:         "abc123456",
			Output: Output{
				KeypairJsonFile:         "keypair.json",
				ModelJsonFile:           "model.json",
				AccountAssertionFile:    "account.assertion",
				AccountKeyAssertionFile: "account-key.assertion",
				ModelAssertionFile:      "model.assertion",
				SerialAssertionFile:     "serial.assertion",
			},
		}
		out, err := yaml.Marshal(config)
		if err != nil {
			log.Println(err)
		}
		log.Println("config file example:")
		log.Println(string(out))
		log.Fatal("You need to provide a config file")
	}

	// load config file
	filename := flag.Arg(0)
	yamlFile, err := ioutil.ReadFile(filename)
	rplib.Checkerr(err)
	err = yaml.Unmarshal(yamlFile, &config)
	if err != nil {
		panic(err)
	}

	// print config
	out, err := yaml.Marshal(config)
	if err != nil {
		panic(err)
	}
	log.Println("config:")
	log.Println(string(out))

	const encodedTestRootAccount = `type: account
authority-id: testrootorg
account-id: testrootorg
display-name: Testrootorg
timestamp: 2016-08-11T18:30:57+02:00
username: testrootorg
validation: certified
sign-key-sha3-384: hIedp1AvrWlcDI4uS_qjoFLzjKl5enu4G2FYJpgB3Pj-tUzGlTQBxMBsBmi-tnJR

AcLBUgQAAQoABgUCV6yoQQAAelEQAEdSECpdmV5a2G5VMBzJFuHQUU1FzgZ7gPQjc3l0BibDWm8O
rDi7IT3L80OkqS2AoQgHS5KtEvKqEmhyfcdzcXgvCkHR5kucRBJJaPy8z6gGMhzZIPlc+EqY+Cvb
/MQPLvtYYvtAxq1vWz+aDGGwk2Z/dFUG+wofvNWodz400gYTZeFOCZwStBD84S7iY/3pMQgC3+SO
QMr/VI+bgmOukFqZL0cX4ReiuUs2W45V6EC81UGBjk+k7AVTEXMR1Xo8f0yiRzlLoEdKQMCOC45Q
n4eedjCToGRPFcktM0QhgfbcpPIQKHNqKGGvtQQXvW5PIZ7AS4rTfQScXTn1dqDsL/ZVdasvOpCP
5o4WvoWMoU8+Hm4n6ckw4sXn//PZIQrtnkp2DO+9JXXZasIPg4k1mvUQ5Kb9qCcBbaM+OO1izOoC
3PY8xHNQNfHNHwBMewhnU2NpdTS0mTepN/8iFsDT1vSZ28OE2hgbu1ltqx4AsRkCVyFFx6N6OYm2
UDNozU9K5w0NY4u9HSTDz4KrBIalAaKY72CIUqeVsmAcYatXglbj7dVTZTw75M0v1thQiSoKFqHw
CHykZ6BJRgminY1FqOg7tvqTwzYM7lwaE3K8JpAyzie7v+OSLSxy1vlwUmT2lT+h1i28/w+r+R3Q
C0QC8xuHSvOv3YRtzKna3smAfRlB
`
	const encodedTestRootAccountKey = `type: account-key
authority-id: testrootorg
public-key-sha3-384: hIedp1AvrWlcDI4uS_qjoFLzjKl5enu4G2FYJpgB3Pj-tUzGlTQBxMBsBmi-tnJR
account-id: testrootorg
since: 2016-08-11T18:30:57+02:00
body-length: 717
sign-key-sha3-384: hIedp1AvrWlcDI4uS_qjoFLzjKl5enu4G2FYJpgB3Pj-tUzGlTQBxMBsBmi-tnJR

AcbBTQRWhcGAARAA8dC6HP+NfM5sNgCHH+bsQv4YLIR8glPfJ+HEXyaYdNO1+oFyX4nx7CpV5Umu
TYs7DPVpToAiN3snpBdPPKu5UEzkQ6OGDucf2bZnAInj7WzKwGnOA/Y/uQMduIyeFZ4mLnUNcF+M
e8LV0aS/pQhEdBUuRxEOi9zlv0p7X1bUs6LIUTubu6+smFtbdBBNOD+0qrvjf7CvsScrTsQswtvw
cLoB4GX94wK6RQrlkmYJPUFZqkdWt7cp0iq8d+Ts8UnT8sgWuFzkMCgBKritS7/545mE8AE0fsyF
Gt5+0jcjgs9LDk5gRO7EgoFLXPsEBdiLdVms7OGAwPGG00wfFYL3ho4PCfKq+mH0kOgUAynlJ7x8
MCR92eWEi/ylHXiO0jnRY8UsutrM76eLN41iUla/6j5DcsXxQB/xzlYkUdtXtYrn6L/DTsnixclu
3ogPzlPEFyVxv0vWIgkKLWXj2JRRt2uqe3K33TvdF0H+m6snZTStn7VY3if9fvyx14+tKh16ucdQ
a1zzJoTKTqYWX9B+ZfENGKJUnhTP0x7Cm6lg3EUGay/b5hsA4DBoqShuf/N0jVLojdhxi3Ck/DBN
lqCD0zy4uzvinjX+b4ay+LKBE3N15AsfEkWIwzI+1OdDlOWWqOxJkM6lrQ5hRQ1fHZoCiGjHbjeE
1RIFO2TAw2tpyUcAEQEAAQ==

AcLBUgQAAQoABgUCV6yoQQAAAaQQAJ+6saqG2DElfKZBbmthhlN8fHXSR8RX5LnbfE5zd4vTbthC
//MjJtpUwq5vpM1/XB9p8cGZD1UlEdUa8l9N8oGSfJARZ+rAsPLlguzSoV4p6ph16HPlvBVt5npB
DqK/Oxw+mtx2cnxn8X9Zw3wyz4mXp3cuu7PwSQvFSvcrxoNIOVkaHYEytQqqvZp8Lq1AirllGEL8
EocRLOiG0O99P3BJytLWLYePRJ6qToiz58WuZEVj2lkC+HqrIoVrjgFAUlq100R15xgc4WtNFdWr
hInauQxco+/vwHvCgxa/Ky+dABY/W+D9fuM7kjrhh/zqQiiIRGhfAndoi9I7Q/FISrECckZEN0yb
N3ntOkTJpCnonTfGW6S0VDfGjQreekEU4nwYk3ewdCDY9n9N4zOPmylqU3u2lLJJNsi9rHWWYTOM
9tXI1yocgrbKaQ8WQQeBQx0SVFdWOl+NvsGcKvs/7qm7SWr/pXo4F+MabqIzX1bb/WvgarpDiGYB
p+ELFp1KRq+vS0qtP1fggrhyGmuQFeSf411cXKa21h870GcaBlmbZZMB/C1lD5fPG1WsrT7DO2Yu
Uhf1Q4y+kAgxqL7zZUqJogpxNgw3He66uB7V7hf/UpOfFNeQZaZDCfSzbz/fNzNvNaqiMh6OUrbd
k9v1ImHrPI6+o+xjCbMc2xdRcvM+
`

	var (
		TestRootAccount asserts.Assertion
	)

	acct, err := asserts.Decode([]byte(encodedTestRootAccount))
	if err != nil {
		panic(fmt.Sprintf("cannot decode trusted assertion: %v", err))
	}
	accKey, err := asserts.Decode([]byte(encodedTestRootAccountKey))
	if err != nil {
		panic(fmt.Sprintf("cannot decode trusted assertion: %v", err))
	}

	TestRootAccount = acct

	rootPrivKey, _ := assertstest.ReadPrivKey(systestkeys.TestRootPrivKey)
	log.Println("Public key id of rootPrivKey: ", rootPrivKey.PublicKey().ID())

	RootAccountID := TestRootAccount.(*asserts.Account).AccountID()
	log.Println("RootAccountID: ", RootAccountID)

	// open db
	fsStore, err := asserts.OpenFSKeypairManager(config.DBStorePath)
	if err != nil {
		panic(err)
	}
	bs, err := asserts.OpenFSBackstore(config.DBStorePath)
	if err != nil {
		panic(err)
	}
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore:      bs,
		KeypairManager: fsStore,
		Trusted: []asserts.Assertion{
			acct, accKey,
		},
	})
	if err != nil {
		panic(err)
	}

	// signing db of authority account
	rootSigning := assertstest.NewSigningDB(RootAccountID, rootPrivKey)

	// query account from db
	var trustedAcct *asserts.Account
	ret, err := db.Find(asserts.AccountType, map[string]string{
		"account-id": config.AccountID,
	})
	if asserts.ErrNotFound == err {
		// not found in db. generate account
		log.Println("Create new account")
		trustedAcct = assertstest.NewAccount(rootSigning, config.AccountID, map[string]interface{}{
			"account-id": config.AccountID,
			"validation": "certified",
			"timestamp":  time.Now().Format(time.RFC3339),
		}, "")
		err = db.Add(trustedAcct)
		if err != nil {
			panic(err)
		}
	} else if err != nil {
		panic(err)
	} else {
		trustedAcct = ret.(*asserts.Account)
	}

	assert := []asserts.Assertion{trustedAcct}
	log.Println("trustedAcct:")
	log.Println(string(asserts.Encode(assert[0])))
	ioutil.WriteFile(config.Output.AccountAssertionFile, asserts.Encode(assert[0]), 0600)

	// load keypair
	var accountPrivKey asserts.PrivateKey
	var trustedKey *asserts.AccountKey
	var armored []byte
	armored, err = ioutil.ReadFile(config.TestKeyFile)
	if err == nil {
		accountPrivKey, _ = assertstest.ReadPrivKey(string(armored))
		log.Println("account key have been read:", accountPrivKey.PublicKey().ID())
	} else {
		// key file not loaded. generate new keypair
		accountPrivKey, armored, err = rplib.GenerateKey(4096)
		rplib.Checkerr(err)

		// export armored private key
		ioutil.WriteFile(config.TestKeyFile, armored, 0600)

		// import new keypair
		err = db.ImportKey(config.AccountID, accountPrivKey)
		rplib.Checkerr(err)
	}

	encodedSigningKey := base64.StdEncoding.EncodeToString(armored)
	keypairJson, err := json.Marshal(service.KeypairWithPrivateKey{PrivateKey: encodedSigningKey, AuthorityID: config.AccountID})
	if err != nil {
		panic(err)
	}
	ioutil.WriteFile("keypair.json", keypairJson, 0600)

	// query account-key from db
	// should be signed by testrootorg
	ret, err = db.Find(asserts.AccountKeyType, map[string]string{
		"account-id":          config.AccountID,
		"public-key-sha3-384": accountPrivKey.PublicKey().ID(),
	})
	if asserts.ErrNotFound == err {
		// create new account-key
		// this assertion is signed by authority key
		trustedKey = assertstest.NewAccountKey(rootSigning, trustedAcct, map[string]interface{}{
			"since": time.Now().Format(time.RFC3339),
		}, accountPrivKey.PublicKey(), "")
		err = db.Add(trustedKey)
		if err != nil {
			panic(err)
		}
	} else if err != nil {
		panic(err)
	} else {
		trustedKey = ret.(*asserts.AccountKey)
	}

	log.Println("trustedKey:")
	assert = []asserts.Assertion{trustedKey}
	log.Println(string(asserts.Encode(assert[0])))
	ioutil.WriteFile(config.Output.AccountKeyAssertionFile, asserts.Encode(assert[0]), 0600)
	log.Println("Public key id of trustedKey: ", trustedKey.PublicKeyID())

	// signing db of developer account
	accountSigning := assertstest.NewSigningDB(config.AccountID, accountPrivKey)
	// generate model assertion signed by account-id
	modelAssertion := rplib.NewModel(accountSigning, map[string]interface{}{
		"series":       "16",
		"authority-id": config.AccountID,
		"brand-id":     config.AccountID,
		"model":        config.Model,
		"revision":     "1",
		"core":         "ubuntu-core",
		"architecture": "amd64",
		"class":        "fixed",
		"gadget":       "pc",
		"kernel":       "pc-kernel",
		"store":        "brand-store",
	}, "")
	ioutil.WriteFile(config.Output.ModelAssertionFile, asserts.Encode(modelAssertion), 0600)

	modelJson, err := json.Marshal(service.ModelSerialize{BrandID: config.AccountID, Name: config.Model, KeypairID: 1})
	if err != nil {
		panic(err)
	}
	ioutil.WriteFile("model.json", modelJson, 0600)

	// load device key
	var devicePrivKey asserts.PrivateKey
	armored, err = ioutil.ReadFile(config.DeviceKeyFile)
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

	// generate serial assertion signed by account-id
	deviceAssertion := rplib.NewDevice(accountSigning, devicePrivKey.PublicKey(), map[string]interface{}{
		"series":       "16",
		"authority-id": config.AccountID,
		"brand-id":     config.AccountID,
		"model":        config.Model,
		"serial":       config.Serial,
		"revision":     "1",
	}, "")
	ioutil.WriteFile(config.Output.SerialAssertionFile, asserts.Encode(deviceAssertion), 0600)
}
