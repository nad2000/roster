package main

import (
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-kit/kit/log"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/nad2000/roster/pb"
	"github.com/nad2000/roster/players"
	"google.golang.org/grpc"
)

func main() {
	logger := createLogger()
	startLogger := log.With(logger, "tag", "start")
	startLogger.Log("msg", "created logger")

	db, err := createDatabase()
	if err != nil {
		startLogger.Log("msg", "failed to connect to database", "err", err)
		os.Exit(1)
	}
	defer db.Close()
	startLogger.Log("msg", "connected to database")

	ps := createPlayersService(db, logger)
	startLogger.Log("msg", "created players service")

	ep := players.NewEndpoints(ps)
	startLogger.Log("msg", "created endpoints")

	errs := make(chan error)

	go func() {
		httpTransport := players.NewHTTPTransport(ep, logger)
		startLogger.Log("msg", "created http transport")

		httpAddr := ":9090"
		startLogger.Log("transport", "http", "address", httpAddr, "msg", "listening")
		errs <- http.ListenAndServe(httpAddr, httpTransport)
	}()

	go func() {
		grpcTransport := players.NewGRPCTransport(ep, logger)
		startLogger.Log("msg", "created grpc transport")

		grpcAddr := ":9091"
		listener, err := net.Listen("tcp", grpcAddr)
		if err != nil {
			errs <- err
			return
		}
		startLogger.Log("msg", "started grpc listener", "address", grpcAddr)

		grpcServer := grpc.NewServer()
		pb.RegisterPlayersServer(grpcServer, grpcTransport)
		startLogger.Log("transport", "grpc", "address", grpcAddr, "msg", "listening")
		errs <- grpcServer.Serve(listener)
	}()

	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		errs <- fmt.Errorf("%s", <-c)
	}()

	logger.Log("terminated", <-errs)
}

func createLogger() log.Logger {
	logger := log.NewLogfmtLogger(log.NewSyncWriter(os.Stdout))
	return log.With(logger, "ts", log.DefaultTimestampUTC())
}

func createDatabase() (*sqlx.DB, error) {
	db, err := sqlx.Connect("postgres", fmt.Sprintf("user=%s password=%s dbname=roster sslmode=disable",
		os.Getenv("ROSTER_USER"),
		os.Getenv("ROSTER_PASSWORD")))
	if err != nil {
		return nil, err
	}

	ddl, err := ioutil.ReadFile("create_table.sql")
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(string(ddl))
	return db, err
}

func createPlayersService(db *sqlx.DB, logger log.Logger) players.Service {
	ps := players.NewService(db)
	ps = players.NewLoggingService(log.With(logger, "tag", "players"), ps)
	return ps
}
