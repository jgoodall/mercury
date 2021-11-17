package info

import (
	"fmt"
	"io/ioutil"
	"path"
	"sort"
	"time"

	"github.com/dgraph-io/badger/v2"
	"github.com/dgraph-io/badger/v2/pb"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/context"

	"code.ornl.gov/situ/mercury/common"
	"code.ornl.gov/situ/mercury/index"
)

func Get(basePath string, showKeys bool) (err error) {
	logger := log.With().Str("component", "info").Str("index-base-path", basePath).Logger()

	labelDirs, err := ioutil.ReadDir(basePath)
	if err != nil {
		return err
	}

	keyMap := make(map[string]struct{})

	// Loop through all of the labels in the basePath.
	for _, label := range labelDirs {
		if !label.IsDir() {
			continue
		}

		// Loop through all of the indices in each of the labels.
		labelDir := path.Join(basePath, label.Name())
		dirs, err := ioutil.ReadDir(labelDir)
		if err != nil {
			return err
		}
		for _, d := range dirs {
			if !d.IsDir() {
				continue
			}

			idxPath := path.Join(labelDir, d.Name())
			fmt.Printf("Index: %s:\n", idxPath)

			var db *badger.DB
			opts := badger.DefaultOptions(idxPath).WithReadOnly(true).WithLogger(&common.BadgerLogger{Logger: logger})
			db, err = badger.Open(opts)
			if err != nil {
				return err
			}
			defer db.Close()

			lsm, vlog := db.Size()
			total := lsm + vlog
			fmt.Printf("Database size (bytes): %d (lsm) / %d (vlog) / %d (total)\n", lsm, vlog, total)
			tables := db.Tables(true)
			for _, table := range tables {
				fmt.Printf("Table (%d) total keys: %d\n", table.ID, table.KeyCount)
			}

			if showKeys {
				dbKeyMap, err := uniqueKeys(db)
				if err != nil {
					return err
				}
				for k := range dbKeyMap {
					if _, ok := keyMap[k]; !ok {
						keyMap[k] = struct{}{}
					}
				}
			}
			fmt.Println()
		}
	}

	if showKeys {
		keys := make([]string, 0)
		for k := range keyMap {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Printf("Unique Keys (%d)\n", len(keys))
		for _, key := range keys {
			fmt.Println(key)
		}
	}

	return
}

// Using badger stream API, get a map of all unique keys.
func uniqueKeys(db *badger.DB) (map[string]struct{}, error) {
	stream := db.NewStream()
	keyMap := make(map[string]struct{})
	stream.Send = func(list *pb.KVList) error {
		for _, kv := range list.GetKv() {
			var k index.Key
			err := k.UnmarshalBinary(kv.GetKey())
			if err != nil {
				return err
			}
			keyMap[string(k.String())] = struct{}{}
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := stream.Orchestrate(ctx)
	if err != nil {
		return nil, err
	}

	return keyMap, nil
}
