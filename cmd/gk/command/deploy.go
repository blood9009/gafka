package command

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/user"
	"strconv"
	"strings"
	"text/template"

	"github.com/funkygao/gafka/ctx"
	"github.com/funkygao/gafka/zk"
	"github.com/funkygao/gocli"
	"github.com/funkygao/golib/color"
	gio "github.com/funkygao/golib/io"
)

// go get -u github.com/jteeuwen/go-bindata
//go:generate go-bindata -nomemcopy -pkg command template/...

type Deploy struct {
	Ui  cli.Ui
	Cmd string

	zkzone        *zk.ZkZone
	kafkaBaseDir  string
	zone, cluster string
	rootPah       string
	user          string
	userInfo      *user.User
	brokerId      string
	tcpPort       string
	ip            string
	demoMode      bool
}

func (this *Deploy) Run(args []string) (exitCode int) {
	cmdFlags := flag.NewFlagSet("deploy", flag.ContinueOnError)
	cmdFlags.Usage = func() { this.Ui.Output(this.Help()) }
	cmdFlags.StringVar(&this.zone, "z", "", "")
	cmdFlags.StringVar(&this.cluster, "c", "", "")
	cmdFlags.StringVar(&this.kafkaBaseDir, "kafka.base", ctx.KafkaHome(), "")
	cmdFlags.StringVar(&this.brokerId, "broker.id", "", "")
	cmdFlags.StringVar(&this.tcpPort, "port", "", "")
	cmdFlags.StringVar(&this.rootPah, "root", "/var/wd", "")
	cmdFlags.StringVar(&this.ip, "ip", "", "")
	cmdFlags.StringVar(&this.user, "user", "sre", "")
	cmdFlags.BoolVar(&this.demoMode, "demo", false, "")
	if err := cmdFlags.Parse(args); err != nil {
		return 1
	}

	if validateArgs(this, this.Ui).
		require("-z", "-c").
		invalid(args) {
		return 2
	}

	this.zkzone = zk.NewZkZone(zk.DefaultConfig(this.zone, ctx.ZoneZkAddrs(this.zone)))
	clusers := this.zkzone.Clusters()
	zkchroot, present := clusers[this.cluster]
	if !present {
		this.Ui.Error(fmt.Sprintf("run 'gk clusters -z %s -add %s -p $zkchroot' first!",
			this.zone, this.cluster))
		return 1
	}

	if this.demoMode {
		this.demo()
		return
	}

	if validateArgs(this, this.Ui).
		require("-broker.id", "-port", "-ip").
		invalid(args) {
		return 2
	}

	if !ctx.CurrentUserIsRoot() {
		this.Ui.Error("requires root priviledges!")
		return 1
	}

	var err error
	this.userInfo, err = user.Lookup(this.user)
	swallow(err)

	// prepare the root directory
	this.rootPah = strings.TrimSuffix(this.rootPah, "/")
	err = os.MkdirAll(fmt.Sprintf("%s/bin", this.instanceDir()), 0755)
	swallow(err)
	this.chown(fmt.Sprintf("%s/bin", this.instanceDir()))
	err = os.MkdirAll(fmt.Sprintf("%s/config", this.instanceDir()), 0755)
	swallow(err)
	this.chown(fmt.Sprintf("%s/config", this.instanceDir()))
	err = os.MkdirAll(fmt.Sprintf("%s/logs", this.instanceDir()), 0755)
	swallow(err)
	this.chown(fmt.Sprintf("%s/logs", this.instanceDir()))

	type templateVar struct {
		KafkaBase   string
		BrokerId    string
		TcpPort     string
		Ip          string
		User        string
		ZkChroot    string
		ZkAddrs     string
		InstanceDir string
	}
	data := templateVar{
		ZkChroot:    zkchroot,
		KafkaBase:   this.kafkaBaseDir,
		BrokerId:    this.brokerId,
		Ip:          this.ip,
		InstanceDir: this.instanceDir(),
		User:        this.user,
		TcpPort:     this.tcpPort,
		ZkAddrs:     this.zkzone.ZkAddrs(),
	}

	// package the kafka runtime together
	if !gio.DirExists(this.kafkaLibDir()) {
		swallow(os.MkdirAll(this.kafkaLibDir(), 0755))
		this.installKafka()
	}

	// bin
	this.writeFileFromTemplate("template/bin/kafka-run-class.sh",
		fmt.Sprintf("%s/bin/kafka-run-class.sh", this.instanceDir()), 0755, data, true)
	this.writeFileFromTemplate("template/bin/kafka-server-start.sh",
		fmt.Sprintf("%s/bin/kafka-server-start.sh", this.instanceDir()), 0755, data, true)
	this.writeFileFromTemplate("template/bin/setenv.sh",
		fmt.Sprintf("%s/bin/setenv.sh", this.instanceDir()), 0755, data, true)

	// /etc/init.d/
	this.writeFileFromTemplate("template/init.d/kafka",
		fmt.Sprintf("/etc/init.d/%s", this.clusterName()), 0755, data, false)

	// config
	this.writeFileFromTemplate("template/config/server.properties",
		fmt.Sprintf("%s/config/server.properties", this.instanceDir()), 0644, data, true)
	this.writeFileFromTemplate("template/config/log4j.properties",
		fmt.Sprintf("%s/config/log4j.properties", this.instanceDir()), 0644, data, true)

	this.Ui.Warn(fmt.Sprintf("deployed! REMEMBER to add monitor for this new broker!"))
	this.Ui.Warn(fmt.Sprintf("NOW, please run the following command:"))
	this.Ui.Output(color.Red("chkconfig --add %s", this.clusterName()))
	this.Ui.Output(color.Red("/etc/init.d/%s start", this.clusterName()))

	return
}

func (this *Deploy) kafkaLibDir() string {
	return fmt.Sprintf("%s/libs", this.kafkaBaseDir)
}

func (this *Deploy) instanceDir() string {
	return fmt.Sprintf("%s/%s", this.rootPah, this.clusterName())
}

func (this *Deploy) clusterName() string {
	return fmt.Sprintf("kfk_%s", this.cluster)
}

func (this *Deploy) installKafka() {
	this.Ui.Output("installing kafka runtime...")
	jars := []string{
		"jopt-simple-3.2.jar",
		"kafka_2.10-0.8.1.1.jar",
		"log4j-1.2.15.jar",
		"metrics-core-2.2.0.jar",
		"scala-library-2.10.1.jar",
		"slf4j-api-1.7.2.jar",
		"snappy-java-1.0.5.jar",
		"zkclient-0.3.jar",
		"zookeeper-3.3.4.jar",
	}
	for _, jar := range jars {
		this.writeFileFromTemplate(
			fmt.Sprintf("template/kafkalibs/%s", jar),
			fmt.Sprintf("%s/libs/%s", this.kafkaBaseDir, jar),
			0644, nil, false)
	}
	this.Ui.Output("kafka runtime installed")
}

func (this *Deploy) writeFileFromTemplate(tplSrc, dst string, perm os.FileMode,
	data interface{}, chown bool) {
	b, err := Asset(tplSrc)
	swallow(err)
	if data != nil {
		wr := &bytes.Buffer{}
		t := template.Must(template.New(tplSrc).Parse(string(b)))
		err = t.Execute(wr, data)
		swallow(err)

		err = ioutil.WriteFile(dst, wr.Bytes(), perm)
		swallow(err)

		return
	}

	// no template, just file copy
	err = ioutil.WriteFile(dst, b, perm)
	swallow(err)

	if chown {
		this.chown(dst)
	}
}

func (this *Deploy) demo() {
	var (
		maxPort     int
		maxBrokerId int
	)

	this.zkzone.ForSortedBrokers(func(cluster string, liveBrokers map[string]*zk.BrokerZnode) {
		for _, broker := range liveBrokers {
			if maxPort < broker.Port {
				maxPort = broker.Port
			}
		}
	})

	brokers := &Brokers{
		Ui:  this.Ui,
		Cmd: this.Cmd,
	}
	maxBrokerId = brokers.maxBrokerId(this.zkzone, this.cluster)

	ip, err := ctx.LocalIP()
	swallow(err)

	this.Ui.Output(fmt.Sprintf("gk deploy -z %s -c %s -broker.id %d -port %d -ip %s",
		this.zone, this.cluster, maxBrokerId+1, maxPort+1, ip.String()))

}

func (this *Deploy) chown(fp string) {
	uid, _ := strconv.Atoi(this.userInfo.Uid)
	gid, _ := strconv.Atoi(this.userInfo.Gid)
	swallow(os.Chown(fp, uid, gid))
}

func (*Deploy) Synopsis() string {
	return "Deploy a new kafka broker"
}

func (this *Deploy) Help() string {
	help := fmt.Sprintf(`
Usage: %s deploy -z zone -c cluster [options]

    Deploy a new kafka broker

Options:

    -demo
      Demonstrate how to use this command.

    -root dir
      Root directory of the kafka broker.
      Defaults to /var/wd

    -ip addr
      Advertised host name of this new broker.	

    -port port
      Tcp port the broker will listen on.

    -broker.id id

    -user runAsUser
      The deployed kafka broker will run as this user.
      Defaults to sre

    -kafka.base dir
      Kafka installation prefix dir.
      Defaults to %s

`, this.Cmd, ctx.KafkaHome())
	return strings.TrimSpace(help)
}
