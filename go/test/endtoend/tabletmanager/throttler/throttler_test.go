/*
Copyright 2019 The Vitess Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package master

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"vitess.io/vitess/go/vt/vttablet/tabletserver/throttle/base"

	"vitess.io/vitess/go/test/endtoend/cluster"

	"github.com/stretchr/testify/assert"
)

var (
	clusterInstance *cluster.LocalProcessCluster
	masterTablet    cluster.Vttablet
	replicaTablet   cluster.Vttablet
	hostname        = "localhost"
	keyspaceName    = "ks"
	cell            = "zone1"
	sqlSchema       = `
	create table t1(
		id bigint,
		value varchar(16),
		primary key(id)
	) Engine=InnoDB;
`

	vSchema = `
	{
    "sharded": true,
    "vindexes": {
      "hash": {
        "type": "hash"
      }
    },
    "tables": {
      "t1": {
        "column_vindexes": [
          {
            "column": "id",
            "name": "hash"
          }
        ]
      }
    }
	}`

	httpClient   = base.SetupHTTPClient(time.Second)
	checkAPIPath = "throttler/check"
)

func TestMain(m *testing.M) {
	defer cluster.PanicHandler(nil)
	flag.Parse()

	exitCode := func() int {
		clusterInstance = cluster.NewCluster(cell, hostname)
		defer clusterInstance.Teardown()

		// Start topo server
		err := clusterInstance.StartTopo()
		if err != nil {
			return 1
		}

		// Set extra tablet args for lock timeout
		clusterInstance.VtTabletExtraArgs = []string{
			"-lock_tables_timeout", "5s",
			"-watch_replication_stream",
			"-enable_replication_reporter",
		}
		// We do not need semiSync for this test case.
		clusterInstance.EnableSemiSync = false

		// Start keyspace
		keyspace := &cluster.Keyspace{
			Name:      keyspaceName,
			SchemaSQL: sqlSchema,
			VSchema:   vSchema,
		}

		if err = clusterInstance.StartUnshardedKeyspace(*keyspace, 1, false); err != nil {
			return 1
		}

		// Collect table paths and ports
		tablets := clusterInstance.Keyspaces[0].Shards[0].Vttablets
		for _, tablet := range tablets {
			if tablet.Type == "master" {
				masterTablet = *tablet
			} else if tablet.Type != "rdonly" {
				replicaTablet = *tablet
			}
		}

		return m.Run()
	}()
	os.Exit(exitCode)
}

func TestThrottlerBeforeMetricsCollected(t *testing.T) {
	defer cluster.PanicHandler(t)

	// Immediately after startup, we expect this response:
	// {"StatusCode":404,"Value":0,"Threshold":0,"Message":"No such metric"}
	resp, err := httpClient.Head(fmt.Sprintf("http://localhost:%d/%s", masterTablet.HTTPPort, checkAPIPath))
	assert.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestThrottlerAfterMetricsCollected(t *testing.T) {
	defer cluster.PanicHandler(t)

	time.Sleep(10 * time.Second)
	// By this time metrics will have been collected. We expect no lag, and something like:
	// {"StatusCode":200,"Value":0.282278,"Threshold":1,"Message":""}
	resp, err := httpClient.Head(fmt.Sprintf("http://localhost:%d/%s", masterTablet.HTTPPort, checkAPIPath))
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
