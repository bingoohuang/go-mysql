package main

import (
	"flag"
	"fmt"
	"github.com/siddontang/go-mysql/replication"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/siddontang/go-mysql/canal"
	"github.com/siddontang/go-mysql/mysql"
)

var host = flag.String("host", "127.0.0.1", "MySQL host")
var port = flag.Int("port", 3306, "MySQL port")
var user = flag.String("user", "root", "MySQL user, must have replication privilege")
var password = flag.String("password", "", "MySQL password")

var flavor = flag.String("flavor", "mysql", "Flavor: mysql or mariadb")

var serverID = flag.Int("server-id", 101, "Unique Server ID")
var mysqldump = flag.String("mysqldump", "mysqldump", "mysqldump execution path")

var dbs = flag.String("dbs", "test", "dump Databases, separated by comma")
var tables = flag.String("Tables", "", "dump Tables, separated by comma, will overwrite dbs")
var tableDB = flag.String("table_db", "test", "database for dump Tables")
var ignoreTables = flag.String("ignore_tables", "", "ignore Tables, must be database.table format, separated by comma")

var startName = flag.String("bin_name", "", "start sync from binlog name")
var startPos = flag.Uint("bin_pos", 0, "start sync from binlog position of")

var heartbeatPeriod = flag.Duration("heartbeat", 60*time.Second, "master heartbeat period")
var readTimeout = flag.Duration("read_timeout", 90*time.Second, "connection read timeout")

func main() {
	flag.Parse()

	cfg := canal.NewDefaultConfig()
	cfg.Addr = fmt.Sprintf("%s:%d", *host, *port)
	cfg.User = *user
	cfg.Password = *password
	cfg.Flavor = *flavor
	cfg.UseDecimal = true

	cfg.ReadTimeout = *readTimeout
	cfg.HeartbeatPeriod = *heartbeatPeriod
	cfg.ServerID = uint32(*serverID)
	cfg.Dump.ExecutionPath = *mysqldump
	cfg.Dump.DiscardErr = false

	c, err := canal.NewCanal(cfg)
	if err != nil {
		fmt.Printf("create canal err %v", err)
		os.Exit(1)
	}

	if len(*ignoreTables) == 0 {
		subs := strings.Split(*ignoreTables, ",")
		for _, sub := range subs {
			if seps := strings.Split(sub, "."); len(seps) == 2 {
				c.AddDumpIgnoreTables(seps[0], seps[1])
			}
		}
	}

	if len(*tables) > 0 && len(*tableDB) > 0 {
		subs := strings.Split(*tables, ",")
		c.AddDumpTables(*tableDB, subs...)
	} else if len(*dbs) > 0 {
		subs := strings.Split(*dbs, ",")
		c.AddDumpDatabases(subs...)
	}

	eventHandler := makeHandler()
	fmt.Printf("use eventHandler %+v\n", *eventHandler)

	c.SetEventHandler(eventHandler)

	startPos := mysql.Position{
		Name: *startName,
		Pos:  uint32(*startPos),
	}

	go func() {
		err = c.RunFrom(startPos)
		if err != nil {
			fmt.Printf("start canal err %v", err)
		}
	}()

	sc := make(chan os.Signal, 1)
	signal.Notify(sc,
		os.Kill,
		os.Interrupt,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT)

	<-sc

	c.Close()
}

type Handler struct {
	Databases map[string]bool

	// dump Tables, separated by comma, will overwrite dbs
	Tables       map[string]bool
	TableDB      string
	IgnoreTables map[string]bool

	canal.DummyEventHandler
}

func makeHandler() *Handler {
	h := &Handler{}
	h.Databases = make(map[string]bool)
	h.Tables = make(map[string]bool)
	h.IgnoreTables = make(map[string]bool)

	if *ignoreTables != "" {
		for _, sub := range strings.Split(*ignoreTables, ",") {
			if seps := strings.Split(sub, "."); len(seps) == 2 {
				h.IgnoreTables[strings.ToUpper(sub)] = true
			}
		}
	}

	if *tables != "" && *tableDB != "" {
		h.TableDB = *tableDB
		for _, sub := range strings.Split(*tables, ",") {
			h.Tables[strings.ToUpper(sub)] = true
		}
	} else if *dbs != "" {
		for _, sub := range strings.Split(*dbs, ",") {
			h.Databases[strings.ToUpper(sub)] = true
		}
	}

	return h
}

func (h *Handler) OnRotate(e *replication.RotateEvent) error {
	fmt.Printf("RotateEvent %s:%d\n", e.NextLogName, e.Position)
	return nil
}

func (h *Handler) OnRow(e *canal.RowsEvent) error {
	if len(h.IgnoreTables) > 0 {
		if _, ok := h.Databases[strings.ToUpper(e.Table.String())]; !ok {
			return nil
		}
	}

	if h.TableDB != "" {
		if strings.ToUpper(e.Table.Schema) != h.TableDB {
			return nil
		}

		if _, ok := h.Tables[strings.ToUpper(e.Table.Name)]; !ok {
			return nil
		}
	} else if len(h.Databases) > 0 {
		if _, ok := h.Databases[strings.ToUpper(e.Table.Schema)]; !ok {
			return nil
		}
	}

	fmt.Printf("RowsEvent: %v\n", e)

	return nil
}

func (h *Handler) String() string {
	return "TestHandler"
}
