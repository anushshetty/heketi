//
// Copyright (c) 2015 The heketi Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package glusterfs

import (
	"fmt"
	"github.com/boltdb/bolt"
	jwt "github.com/dgrijalva/jwt-go"
	"github.com/gorilla/context"
	"github.com/gorilla/mux"
	"github.com/heketi/heketi/executors"
	"github.com/heketi/heketi/executors/mockexec"
	"github.com/heketi/heketi/executors/sshexec"
	"github.com/heketi/rest"
	"github.com/heketi/utils"
	"io"
	"net/http"
	"time"
)

const (
	ASYNC_ROUTE           = "/queue"
	BOLTDB_BUCKET_CLUSTER = "CLUSTER"
	BOLTDB_BUCKET_NODE    = "NODE"
	BOLTDB_BUCKET_VOLUME  = "VOLUME"
	BOLTDB_BUCKET_DEVICE  = "DEVICE"
	BOLTDB_BUCKET_BRICK   = "BRICK"
)

var (
	logger     = utils.NewLogger("[heketi]", utils.LEVEL_DEBUG)
	dbfilename = "heketi.db"
)

type App struct {
	asyncManager *rest.AsyncHttpManager
	db           *bolt.DB
	executor     executors.Executor
	allocator    Allocator
	conf         *GlusterFSConfig

	// For testing only.  Keep access to the object
	// not through the interface
	xo *mockexec.MockExecutor
}

func NewApp(configIo io.Reader) *App {
	app := &App{}

	// Load configuration file
	app.conf = loadConfiguration(configIo)
	if app.conf == nil {
		return nil
	}

	// Setup asynchronous manager
	app.asyncManager = rest.NewAsyncHttpManager(ASYNC_ROUTE)

	// Setup executor
	switch {
	case app.conf.Executor == "mock":
		app.xo = mockexec.NewMockExecutor()
		app.executor = app.xo
	case app.conf.Executor == "ssh" || app.conf.Executor == "":
		app.executor = sshexec.NewSshExecutor(&app.conf.SshConfig)
	default:
		return nil
	}
	if app.executor == nil {
		return nil
	}
	logger.Info("Loaded %v executor", app.conf.Executor)

	// Set db is set in the configuration file
	if app.conf.DBfile != "" {
		dbfilename = app.conf.DBfile
	}

	// Setup BoltDB database
	var err error
	app.db, err = bolt.Open(dbfilename, 0600, &bolt.Options{Timeout: 3 * time.Second})
	if err != nil {
		logger.LogError("Unable to open database")
		return nil
	}

	err = app.db.Update(func(tx *bolt.Tx) error {
		// Create Cluster Bucket
		_, err := tx.CreateBucketIfNotExists([]byte(BOLTDB_BUCKET_CLUSTER))
		if err != nil {
			logger.LogError("Unable to create cluster bucket in DB")
			return err
		}

		// Create Node Bucket
		_, err = tx.CreateBucketIfNotExists([]byte(BOLTDB_BUCKET_NODE))
		if err != nil {
			logger.LogError("Unable to create cluster bucket in DB")
			return err
		}

		// Create Volume Bucket
		_, err = tx.CreateBucketIfNotExists([]byte(BOLTDB_BUCKET_VOLUME))
		if err != nil {
			logger.LogError("Unable to create cluster bucket in DB")
			return err
		}

		// Create Device Bucket
		_, err = tx.CreateBucketIfNotExists([]byte(BOLTDB_BUCKET_DEVICE))
		if err != nil {
			logger.LogError("Unable to create cluster bucket in DB")
			return err
		}

		// Create Brick Bucket
		_, err = tx.CreateBucketIfNotExists([]byte(BOLTDB_BUCKET_BRICK))
		if err != nil {
			logger.LogError("Unable to create cluster bucket in DB")
			return err
		}

		return nil

	})
	if err != nil {
		logger.Err(err)
		return nil
	}

	// Set advanced settings
	app.setAdvSettings()

	// Setup allocator
	switch {
	case app.conf.Allocator == "mock":
		app.allocator = NewMockAllocator(app.db)
	case app.conf.Allocator == "simple" || app.conf.Allocator == "":
		app.conf.Allocator = "simple"
		app.allocator = NewSimpleAllocatorFromDb(app.db)
	default:
		return nil
	}
	logger.Info("Loaded %v allocator", app.conf.Allocator)

	// Show application has loaded
	logger.Info("GlusterFS Application Loaded")

	return app
}

func (a *App) setAdvSettings() {
	if a.conf.BrickMaxNum != 0 {
		logger.Info("Adv: Max bricks per volume set to %v", a.conf.BrickMaxNum)

		// From volume_entry.go
		BrickMaxNum = a.conf.BrickMaxNum
	}
	if a.conf.BrickMaxSize != 0 {
		logger.Info("Adv: Max brick size %v GB", a.conf.BrickMaxSize)

		// From volume_entry.go
		// Convert to KB
		BrickMaxSize = uint64(a.conf.BrickMaxSize) * 1024 * 1024
	}
	if a.conf.BrickMinSize != 0 {
		logger.Info("Adv: Min brick size %v GB", a.conf.BrickMinSize)

		// From volume_entry.go
		// Convert to KB
		BrickMinSize = uint64(a.conf.BrickMinSize) * 1024 * 1024
	}
}

// Register Routes
func (a *App) SetRoutes(router *mux.Router) error {

	routes := rest.Routes{

		// HelloWorld
		rest.Route{
			Name:        "Hello",
			Method:      "GET",
			Pattern:     "/hello",
			HandlerFunc: a.Hello},

		// Asynchronous Manager
		rest.Route{
			Name:        "Async",
			Method:      "GET",
			Pattern:     ASYNC_ROUTE + "/{id:[A-Fa-f0-9]+}",
			HandlerFunc: a.asyncManager.HandlerStatus},

		// Cluster
		rest.Route{
			Name:        "ClusterCreate",
			Method:      "POST",
			Pattern:     "/clusters",
			HandlerFunc: a.ClusterCreate},
		rest.Route{
			Name:        "ClusterInfo",
			Method:      "GET",
			Pattern:     "/clusters/{id:[A-Fa-f0-9]+}",
			HandlerFunc: a.ClusterInfo},
		rest.Route{
			Name:        "ClusterList",
			Method:      "GET",
			Pattern:     "/clusters",
			HandlerFunc: a.ClusterList},
		rest.Route{
			Name:        "ClusterDelete",
			Method:      "DELETE",
			Pattern:     "/clusters/{id:[A-Fa-f0-9]+}",
			HandlerFunc: a.ClusterDelete},

		// Node
		rest.Route{
			Name:        "NodeAdd",
			Method:      "POST",
			Pattern:     "/nodes",
			HandlerFunc: a.NodeAdd},
		rest.Route{
			Name:        "NodeInfo",
			Method:      "GET",
			Pattern:     "/nodes/{id:[A-Fa-f0-9]+}",
			HandlerFunc: a.NodeInfo},
		rest.Route{
			Name:        "NodeDelete",
			Method:      "DELETE",
			Pattern:     "/nodes/{id:[A-Fa-f0-9]+}",
			HandlerFunc: a.NodeDelete},

		// Devices
		rest.Route{
			Name:        "DeviceAdd",
			Method:      "POST",
			Pattern:     "/devices",
			HandlerFunc: a.DeviceAdd},
		rest.Route{
			Name:        "DeviceInfo",
			Method:      "GET",
			Pattern:     "/devices/{id:[A-Fa-f0-9]+}",
			HandlerFunc: a.DeviceInfo},
		rest.Route{
			Name:        "DeviceDelete",
			Method:      "DELETE",
			Pattern:     "/devices/{id:[A-Fa-f0-9]+}",
			HandlerFunc: a.DeviceDelete},

		// Volume
		rest.Route{
			Name:        "VolumeCreate",
			Method:      "POST",
			Pattern:     "/volumes",
			HandlerFunc: a.VolumeCreate},
		rest.Route{
			Name:        "VolumeInfo",
			Method:      "GET",
			Pattern:     "/volumes/{id:[A-Fa-f0-9]+}",
			HandlerFunc: a.VolumeInfo},
		rest.Route{
			Name:        "VolumeExpand",
			Method:      "POST",
			Pattern:     "/volumes/{id:[A-Fa-f0-9]+}/expand",
			HandlerFunc: a.VolumeExpand},
		rest.Route{
			Name:        "VolumeDelete",
			Method:      "DELETE",
			Pattern:     "/volumes/{id:[A-Fa-f0-9]+}",
			HandlerFunc: a.VolumeDelete},
		rest.Route{
			Name:        "VolumeList",
			Method:      "GET",
			Pattern:     "/volumes",
			HandlerFunc: a.VolumeList},
	}

	// Register all routes from the App
	for _, route := range routes {

		// Add routes from the table
		router.
			Methods(route.Method).
			Path(route.Pattern).
			Name(route.Name).
			Handler(route.HandlerFunc)

	}

	return nil

}

func (a *App) Close() {

	// Close the DB
	a.db.Close()
	logger.Info("Closed")
}

func (a *App) Hello(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "HelloWorld from GlusterFS Application")
}

// Middleware function
func (a *App) Auth(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {

	// Value saved by the JWT middleware.
	data := context.Get(r, "jwt")

	// Need to change from interface{} to the jwt.Token type
	token := data.(*jwt.Token)

	// Check access
	if "user" == token.Claims["iss"] && r.URL.Path != "/volumes" {
		http.Error(w, "Adminitrator access required", http.StatusUnauthorized)
		return
	}

	// Everything is clean
	next(w, r)
}
