package main

import (
	"sync"

	"time"

	influx "github.com/influxdata/influxdb/client/v2"
)

var (
	influxClientValid     bool
	influxClient          influx.Client
	batchMutex            sync.Mutex
	currentBatch          influx.BatchPoints
	currentBatchSize      int
	currentBatchStartTime time.Time
)

func initInflux() error {
	var (
		err error
	)

	// Create a new HTTPClient
	influxClient, err = influx.NewHTTPClient(influx.HTTPConfig{
		Addr:     Config.InfluxDB.Host,
		Username: Config.InfluxDB.User,
		Password: Config.InfluxDB.Pass,
	})
	if err != nil {
		logger.Errorf("Error creating InfluxDB client: %s", err)
		return err
	}
	influxClientValid = true
	makeBatch()
	return err
}

func closeInflux() {
	batchMutex.Lock()
	currentBatchSize = -1 // Sentinel value
	batchMutex.Unlock()
	_ = influxClient.Close()
	influxClientValid = false
}

// WARNING: the caller should have a lock on batchMutex before calling this
func makeBatch() {
	var err error
	currentBatch, err = influx.NewBatchPoints(influx.BatchPointsConfig{
		Database:  Config.InfluxDB.DB,
		Precision: "ns",
	})
	currentBatchSize = 0
	currentBatchStartTime = time.Now()
	if err != nil {
		logger.Fatalf("Error creating InfluxDB batch points: %s", err)
	}
}

func saveInfluxNumTrunkChannels(numChannels int, TrunkTag string) {
	var (
		pt  *influx.Point
		err error
		now time.Time
	)
	if !influxClientValid {
		return
	}
	batchMutex.Lock()
	defer batchMutex.Unlock()
	if currentBatchSize == -1 {
		// influx connection was closed -- we're probably shutting down, so just ignore
		// any calls to save values
		return
	}
	tags1 := map[string]string{
		"PBX":   Config.InfluxDB.PBXTag,
		"TRUNK": TrunkTag,
	}
	fields := make(map[string]interface{}, 12)
	fields["Inuse"] = numChannels
	now = time.Now()
	pt, err = influx.NewPoint(
		"ChannelCounts",
		tags1,
		fields,
		now,
	)
	if err != nil {
		logger.Errorf("Unable to create new InfluxDB Point, error: %s", err)
		return
	}
	currentBatch.AddPoint(pt)
	currentBatchSize++
	age := now.Sub(currentBatchStartTime)
	if currentBatchSize > 127 || age > (30*time.Second) {
		saveBatch := currentBatch
		go func() {
			if err := influxClient.Write(saveBatch); err != nil {
				logger.Errorf("ERROR: Error %s writing InfluxDB stats.  Possible metric loss.", err)
			}
		}()
		makeBatch()
	}
}
