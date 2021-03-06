// Copyright 2016 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package docker

import (
	"io/ioutil"
	"net/http"
	"regexp"
	"strings"

	dtesting "github.com/fsouza/go-dockerclient/testing"
	"github.com/tsuru/docker-cluster/cluster"
	"github.com/tsuru/tsuru/action"
	"github.com/tsuru/tsuru/app"
	"github.com/tsuru/tsuru/net"
	"github.com/tsuru/tsuru/provision"
	"github.com/tsuru/tsuru/provision/docker/container"
	"github.com/tsuru/tsuru/provision/provisiontest"
	"github.com/tsuru/tsuru/safe"
	"gopkg.in/check.v1"
	"gopkg.in/mgo.v2/bson"
)

func (s *S) TestMoveContainers(c *check.C) {
	p, err := s.startMultipleServersCluster()
	c.Assert(err, check.IsNil)
	err = s.newFakeImage(p, "tsuru/app-myapp", nil)
	c.Assert(err, check.IsNil)
	appInstance := provisiontest.NewFakeApp("myapp", "python", 0)
	defer p.Destroy(appInstance)
	p.Provision(appInstance)
	coll := p.Collection()
	defer coll.Close()
	coll.Insert(container.Container{
		ID:      "container-id",
		AppName: appInstance.GetName(),
		Version: "container-version",
		Image:   "tsuru/python",
	})
	defer coll.RemoveAll(bson.M{"appname": appInstance.GetName()})
	imageId, err := appCurrentImageName(appInstance.GetName())
	c.Assert(err, check.IsNil)
	_, err = addContainersWithHost(&changeUnitsPipelineArgs{
		toHost:      "localhost",
		toAdd:       map[string]*containersToAdd{"web": {Quantity: 2}},
		app:         appInstance,
		imageId:     imageId,
		provisioner: p,
	})
	c.Assert(err, check.IsNil)
	appStruct := &app.App{
		Name: appInstance.GetName(),
	}
	err = s.storage.Apps().Insert(appStruct)
	c.Assert(err, check.IsNil)
	buf := safe.NewBuffer(nil)
	err = p.MoveContainers("localhost", "127.0.0.1", buf)
	c.Assert(err, check.IsNil)
	containers, err := p.listContainersByHost("localhost")
	c.Assert(containers, check.HasLen, 0)
	containers, err = p.listContainersByHost("127.0.0.1")
	c.Assert(containers, check.HasLen, 2)
	parts := strings.Split(buf.String(), "\n")
	c.Assert(parts[0], check.Matches, ".*Moving 2 units.*")
	var matches int
	movingRegexp := regexp.MustCompile(`.*Moving unit.*for.*myapp.*localhost.*127.0.0.1.*`)
	for _, line := range parts[1:] {
		if movingRegexp.MatchString(line) {
			matches++
		}
	}
	c.Assert(matches, check.Equals, 2)
}

func (s *S) TestMoveContainersUnknownDest(c *check.C) {
	p, err := s.startMultipleServersCluster()
	c.Assert(err, check.IsNil)
	err = s.newFakeImage(p, "tsuru/app-myapp", nil)
	c.Assert(err, check.IsNil)
	appInstance := provisiontest.NewFakeApp("myapp", "python", 0)
	defer p.Destroy(appInstance)
	p.Provision(appInstance)
	coll := p.Collection()
	defer coll.Close()
	coll.Insert(container.Container{ID: "container-id", AppName: appInstance.GetName(), Version: "container-version", Image: "tsuru/python"})
	defer coll.RemoveAll(bson.M{"appname": appInstance.GetName()})
	imageId, err := appCurrentImageName(appInstance.GetName())
	c.Assert(err, check.IsNil)
	_, err = addContainersWithHost(&changeUnitsPipelineArgs{
		toHost:      "localhost",
		toAdd:       map[string]*containersToAdd{"web": {Quantity: 2}},
		app:         appInstance,
		imageId:     imageId,
		provisioner: p,
	})
	c.Assert(err, check.IsNil)
	appStruct := &app.App{
		Name: appInstance.GetName(),
	}
	err = s.storage.Apps().Insert(appStruct)
	c.Assert(err, check.IsNil)
	buf := safe.NewBuffer(nil)
	err = p.MoveContainers("localhost", "unknown", buf)
	c.Assert(err, check.Equals, containerMovementErr)
	parts := strings.Split(buf.String(), "\n")
	c.Assert(parts, check.HasLen, 6)
	c.Assert(parts[0], check.Matches, ".*Moving 2 units.*")
	var matches int
	errorRegexp := regexp.MustCompile(`(?s).*Error moving unit.*Caused by:.*unknown.*not found`)
	for _, line := range parts[2:] {
		if errorRegexp.MatchString(line) {
			matches++
		}
	}
	c.Assert(matches, check.Equals, 2)
}

func (s *S) TestMoveContainer(c *check.C) {
	p, err := s.startMultipleServersCluster()
	c.Assert(err, check.IsNil)
	err = s.newFakeImage(p, "tsuru/app-myapp", nil)
	c.Assert(err, check.IsNil)
	appInstance := provisiontest.NewFakeApp("myapp", "python", 0)
	defer p.Destroy(appInstance)
	p.Provision(appInstance)
	coll := p.Collection()
	defer coll.Close()
	coll.Insert(container.Container{
		ID:      "container-id",
		AppName: appInstance.GetName(),
		Version: "container-version",
		Image:   "tsuru/python",
	})
	defer coll.RemoveAll(bson.M{"appname": appInstance.GetName()})
	imageId, err := appCurrentImageName(appInstance.GetName())
	c.Assert(err, check.IsNil)
	addedConts, err := addContainersWithHost(&changeUnitsPipelineArgs{
		toHost:      "localhost",
		toAdd:       map[string]*containersToAdd{"web": {Quantity: 2}},
		app:         appInstance,
		imageId:     imageId,
		provisioner: p,
	})
	c.Assert(err, check.IsNil)
	appStruct := &app.App{
		Name: appInstance.GetName(),
	}
	err = s.storage.Apps().Insert(appStruct)
	c.Assert(err, check.IsNil)
	buf := safe.NewBuffer(nil)
	var serviceBodies []string
	var serviceMethods []string
	rollback := s.addServiceInstance(c, appInstance.GetName(), []string{addedConts[0].ID}, func(w http.ResponseWriter, r *http.Request) {
		data, _ := ioutil.ReadAll(r.Body)
		serviceBodies = append(serviceBodies, string(data))
		serviceMethods = append(serviceMethods, r.Method)
		w.WriteHeader(http.StatusOK)
	})
	defer rollback()
	_, err = p.moveContainer(addedConts[0].ID[:6], "127.0.0.1", buf)
	c.Assert(err, check.IsNil)
	containers, err := p.listContainersByHost("localhost")
	c.Assert(containers, check.HasLen, 1)
	containers, err = p.listContainersByHost("127.0.0.1")
	c.Assert(containers, check.HasLen, 1)
	c.Assert(serviceBodies, check.HasLen, 2)
	c.Assert(serviceMethods, check.HasLen, 2)
	c.Assert(serviceMethods[0], check.Equals, "POST")
	c.Assert(serviceBodies[0], check.Matches, ".*unit-host=127.0.0.1")
	c.Assert(serviceMethods[1], check.Equals, "DELETE")
	c.Assert(serviceBodies[1], check.Matches, ".*unit-host=localhost")
}

func (s *S) TestRebalanceContainers(c *check.C) {
	p, err := s.startMultipleServersCluster()
	c.Assert(err, check.IsNil)
	err = s.newFakeImage(p, "tsuru/app-myapp", nil)
	c.Assert(err, check.IsNil)
	appInstance := provisiontest.NewFakeApp("myapp", "python", 0)
	defer p.Destroy(appInstance)
	p.Provision(appInstance)
	imageId, err := appCurrentImageName(appInstance.GetName())
	c.Assert(err, check.IsNil)
	_, err = addContainersWithHost(&changeUnitsPipelineArgs{
		toHost:      "localhost",
		toAdd:       map[string]*containersToAdd{"web": {Quantity: 5}},
		app:         appInstance,
		imageId:     imageId,
		provisioner: p,
	})
	c.Assert(err, check.IsNil)
	appStruct := &app.App{
		Name: appInstance.GetName(),
	}
	err = s.storage.Apps().Insert(appStruct)
	c.Assert(err, check.IsNil)
	buf := safe.NewBuffer(nil)
	err = p.rebalanceContainers(buf, false)
	c.Assert(err, check.IsNil)
	c1, err := p.listContainersByHost("localhost")
	c.Assert(err, check.IsNil)
	c2, err := p.listContainersByHost("127.0.0.1")
	c.Assert(err, check.IsNil)
	c.Assert((len(c1) == 3 && len(c2) == 2) || (len(c1) == 2 && len(c2) == 3), check.Equals, true)
}

func (s *S) TestRebalanceContainersSegScheduler(c *check.C) {
	otherServer, err := dtesting.NewServer("localhost:0", nil, nil)
	c.Assert(err, check.IsNil)
	defer otherServer.Stop()
	otherUrl := strings.Replace(otherServer.URL(), "127.0.0.1", "localhost", 1)
	p := &dockerProvisioner{}
	err = p.Initialize()
	c.Assert(err, check.IsNil)
	p.storage = &cluster.MapStorage{}
	p.scheduler = &segregatedScheduler{provisioner: p}
	p.cluster, err = cluster.New(p.scheduler, p.storage,
		cluster.Node{Address: s.server.URL(), Metadata: map[string]string{"pool": "pool1"}},
		cluster.Node{Address: otherUrl, Metadata: map[string]string{"pool": "pool1"}},
	)
	c.Assert(err, check.IsNil)
	opts := provision.AddPoolOptions{Name: "pool1"}
	err = provision.AddPool(opts)
	c.Assert(err, check.IsNil)
	err = provision.AddTeamsToPool("pool1", []string{"team1"})
	c.Assert(err, check.IsNil)
	err = s.newFakeImage(p, "tsuru/app-myapp", nil)
	c.Assert(err, check.IsNil)
	appInstance := provisiontest.NewFakeApp("myapp", "python", 0)
	defer p.Destroy(appInstance)
	p.Provision(appInstance)
	imageId, err := appCurrentImageName(appInstance.GetName())
	c.Assert(err, check.IsNil)
	_, err = addContainersWithHost(&changeUnitsPipelineArgs{
		toHost:      "localhost",
		toAdd:       map[string]*containersToAdd{"web": {Quantity: 5}},
		app:         appInstance,
		imageId:     imageId,
		provisioner: p,
	})
	c.Assert(err, check.IsNil)
	appStruct := &app.App{
		Name:      appInstance.GetName(),
		TeamOwner: "team1",
		Pool:      "pool1",
	}
	err = s.storage.Apps().Insert(appStruct)
	c.Assert(err, check.IsNil)
	c1, err := p.listContainersByHost("localhost")
	c.Assert(err, check.IsNil)
	c.Assert(c1, check.HasLen, 5)
	buf := safe.NewBuffer(nil)
	err = p.rebalanceContainers(buf, false)
	c.Assert(err, check.IsNil)
	c.Assert(p.scheduler.ignoredContainers, check.IsNil)
	c1, err = p.listContainersByHost("localhost")
	c.Assert(err, check.IsNil)
	c2, err := p.listContainersByHost("127.0.0.1")
	c.Assert(err, check.IsNil)
	c.Assert((len(c1) == 2 && len(c2) == 3) || (len(c1) == 3 && len(c2) == 2), check.Equals, true)
}

func (s *S) TestRebalanceContainersByHost(c *check.C) {
	otherServer, err := dtesting.NewServer("localhost:0", nil, nil)
	c.Assert(err, check.IsNil)
	defer otherServer.Stop()
	otherUrl := strings.Replace(otherServer.URL(), "127.0.0.1", "localhost", 1)
	p := &dockerProvisioner{}
	err = p.Initialize()
	c.Assert(err, check.IsNil)
	p.storage = &cluster.MapStorage{}
	p.scheduler = &segregatedScheduler{provisioner: p}
	p.cluster, err = cluster.New(p.scheduler, p.storage,
		cluster.Node{Address: s.server.URL(), Metadata: map[string]string{"pool": "pool1"}},
		cluster.Node{Address: otherUrl, Metadata: map[string]string{"pool": "pool1"}},
	)
	c.Assert(err, check.IsNil)
	opts := provision.AddPoolOptions{Name: "pool1"}
	err = provision.AddPool(opts)
	c.Assert(err, check.IsNil)
	err = provision.AddTeamsToPool("pool1", []string{"team1"})
	c.Assert(err, check.IsNil)
	err = s.newFakeImage(p, "tsuru/app-myapp", nil)
	c.Assert(err, check.IsNil)
	appInstance := provisiontest.NewFakeApp("myapp", "python", 0)
	defer p.Destroy(appInstance)
	p.Provision(appInstance)
	imageId, err := appCurrentImageName(appInstance.GetName())
	c.Assert(err, check.IsNil)
	_, err = addContainersWithHost(&changeUnitsPipelineArgs{
		toHost:      "localhost",
		toAdd:       map[string]*containersToAdd{"web": {Quantity: 5}},
		app:         appInstance,
		imageId:     imageId,
		provisioner: p,
	})
	c.Assert(err, check.IsNil)
	appStruct := &app.App{
		Name:      appInstance.GetName(),
		TeamOwner: "team1",
		Pool:      "pool1",
	}
	err = s.storage.Apps().Insert(appStruct)
	c.Assert(err, check.IsNil)
	c1, err := p.listContainersByHost("localhost")
	c.Assert(err, check.IsNil)
	c.Assert(c1, check.HasLen, 5)
	c2, err := p.listContainersByHost("127.0.0.1")
	c.Assert(err, check.IsNil)
	c.Assert(c2, check.HasLen, 0)
	err = p.Cluster().Unregister(otherUrl)
	c.Assert(err, check.IsNil)
	buf := safe.NewBuffer(nil)
	err = p.rebalanceContainersByHost(net.URLToHost(otherUrl), buf)
	c.Assert(err, check.IsNil)
	c.Assert(p.scheduler.ignoredContainers, check.IsNil)
	c2, err = p.listContainersByHost("127.0.0.1")
	c.Assert(err, check.IsNil)
	c.Assert(c2, check.HasLen, 5)
}

func (s *S) TestAppLocker(c *check.C) {
	appName := "myapp"
	appDB := &app.App{Name: appName}
	err := s.storage.Apps().Insert(appDB)
	c.Assert(err, check.IsNil)
	locker := &appLocker{}
	hasLock := locker.Lock(appName)
	c.Assert(hasLock, check.Equals, true)
	c.Assert(locker.refCount[appName], check.Equals, 1)
	appDB, err = app.GetByName(appName)
	c.Assert(err, check.IsNil)
	c.Assert(appDB.Lock.Locked, check.Equals, true)
	c.Assert(appDB.Lock.Owner, check.Equals, app.InternalAppName)
	c.Assert(appDB.Lock.Reason, check.Equals, "container-move")
	hasLock = locker.Lock(appName)
	c.Assert(hasLock, check.Equals, true)
	c.Assert(locker.refCount[appName], check.Equals, 2)
	locker.Unlock(appName)
	c.Assert(locker.refCount[appName], check.Equals, 1)
	appDB, err = app.GetByName(appName)
	c.Assert(err, check.IsNil)
	c.Assert(appDB.Lock.Locked, check.Equals, true)
	locker.Unlock(appName)
	c.Assert(locker.refCount[appName], check.Equals, 0)
	appDB, err = app.GetByName(appName)
	c.Assert(err, check.IsNil)
	c.Assert(appDB.Lock.Locked, check.Equals, false)
}

func (s *S) TestAppLockerBlockOtherLockers(c *check.C) {
	appName := "myapp"
	appDB := &app.App{Name: appName}
	err := s.storage.Apps().Insert(appDB)
	c.Assert(err, check.IsNil)
	locker := &appLocker{}
	hasLock := locker.Lock(appName)
	c.Assert(hasLock, check.Equals, true)
	c.Assert(locker.refCount[appName], check.Equals, 1)
	appDB, err = app.GetByName(appName)
	c.Assert(err, check.IsNil)
	c.Assert(appDB.Lock.Locked, check.Equals, true)
	otherLocker := &appLocker{}
	hasLock = otherLocker.Lock(appName)
	c.Assert(hasLock, check.Equals, false)
}

func (s *S) TestRebalanceContainersManyApps(c *check.C) {
	p, err := s.startMultipleServersCluster()
	c.Assert(err, check.IsNil)
	err = s.newFakeImage(p, "tsuru/app-myapp", nil)
	c.Assert(err, check.IsNil)
	err = s.newFakeImage(p, "tsuru/app-otherapp", nil)
	c.Assert(err, check.IsNil)
	appInstance := provisiontest.NewFakeApp("myapp", "python", 0)
	defer p.Destroy(appInstance)
	p.Provision(appInstance)
	appInstance2 := provisiontest.NewFakeApp("otherapp", "python", 0)
	defer p.Destroy(appInstance2)
	p.Provision(appInstance2)
	imageId, err := appCurrentImageName(appInstance.GetName())
	c.Assert(err, check.IsNil)
	_, err = addContainersWithHost(&changeUnitsPipelineArgs{
		toHost:      "localhost",
		toAdd:       map[string]*containersToAdd{"web": {Quantity: 1}},
		app:         appInstance,
		imageId:     imageId,
		provisioner: p,
	})
	c.Assert(err, check.IsNil)
	imageId2, err := appCurrentImageName(appInstance2.GetName())
	c.Assert(err, check.IsNil)
	_, err = addContainersWithHost(&changeUnitsPipelineArgs{
		toHost:      "localhost",
		toAdd:       map[string]*containersToAdd{"web": {Quantity: 1}},
		app:         appInstance2,
		imageId:     imageId2,
		provisioner: p,
	})
	c.Assert(err, check.IsNil)
	appStruct := &app.App{
		Name: appInstance.GetName(),
	}
	err = s.storage.Apps().Insert(appStruct)
	c.Assert(err, check.IsNil)
	appStruct2 := &app.App{
		Name: appInstance2.GetName(),
	}
	err = s.storage.Apps().Insert(appStruct2)
	c.Assert(err, check.IsNil)
	buf := safe.NewBuffer(nil)
	c1, err := p.listContainersByHost("localhost")
	c.Assert(c1, check.HasLen, 2)
	err = p.rebalanceContainers(buf, false)
	c.Assert(err, check.IsNil)
	c1, err = p.listContainersByHost("localhost")
	c.Assert(c1, check.HasLen, 1)
	c2, err := p.listContainersByHost("127.0.0.1")
	c.Assert(c2, check.HasLen, 1)
}

func (s *S) TestRebalanceContainersDry(c *check.C) {
	p, err := s.startMultipleServersCluster()
	c.Assert(err, check.IsNil)
	err = s.newFakeImage(p, "tsuru/app-myapp", nil)
	c.Assert(err, check.IsNil)
	appInstance := provisiontest.NewFakeApp("myapp", "python", 0)
	defer p.Destroy(appInstance)
	p.Provision(appInstance)
	imageId, err := appCurrentImageName(appInstance.GetName())
	c.Assert(err, check.IsNil)
	args := changeUnitsPipelineArgs{
		app:         appInstance,
		toAdd:       map[string]*containersToAdd{"web": {Quantity: 5}},
		imageId:     imageId,
		provisioner: p,
		toHost:      "localhost",
	}
	pipeline := action.NewPipeline(
		&provisionAddUnitsToHost,
		&bindAndHealthcheck,
		&addNewRoutes,
		&setRouterHealthcheck,
		&updateAppImage,
	)
	err = pipeline.Execute(args)
	c.Assert(err, check.IsNil)
	appStruct := &app.App{
		Name: appInstance.GetName(),
	}
	err = s.storage.Apps().Insert(appStruct)
	c.Assert(err, check.IsNil)
	router, err := getRouterForApp(appInstance)
	c.Assert(err, check.IsNil)
	beforeRoutes, err := router.Routes(appStruct.Name)
	c.Assert(err, check.IsNil)
	c.Assert(beforeRoutes, check.HasLen, 5)
	var serviceCalled bool
	rollback := s.addServiceInstance(c, appInstance.GetName(), nil, func(w http.ResponseWriter, r *http.Request) {
		serviceCalled = true
		w.WriteHeader(http.StatusOK)
	})
	defer rollback()
	buf := safe.NewBuffer(nil)
	err = p.rebalanceContainers(buf, true)
	c.Assert(err, check.IsNil)
	c1, err := p.listContainersByHost("localhost")
	c.Assert(err, check.IsNil)
	c2, err := p.listContainersByHost("127.0.0.1")
	c.Assert(err, check.IsNil)
	c.Assert(c1, check.HasLen, 5)
	c.Assert(c2, check.HasLen, 0)
	routes, err := router.Routes(appStruct.Name)
	c.Assert(err, check.IsNil)
	c.Assert(routes, check.DeepEquals, beforeRoutes)
	c.Assert(serviceCalled, check.Equals, false)
}
