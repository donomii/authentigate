package main

import (
	"github.com/philippgille/gokv"
	"github.com/philippgille/gokv/encoding"
	"github.com/philippgille/gokv/file"
)

type authDB struct {
	Users, ForeignIDs, SessionTokens, UserNames gokv.Store
	ByNames                                     map[string]gokv.Store
}

var b *authDB

// Wrap basic hash functions:  open/exists/put/get
//
// To switch to another keyval store, e.g. AWS, we just replace the API calls here
// Create and open the authentication keyval store
func newAuthDB(filename string) (s *authDB, err error, shutdownFunc func()) {
	s = &authDB{ByNames: make(map[string]gokv.Store)}
	/*
	s.db, err = bolt.Open(filename, 0600, &bolt.Options{Timeout: 1 * time.Second})


		options := bbolt.DefaultOptions
		options.Path = db_prefix + "users"
		options.BucketName = "users"
		s.Users, err = bbolt.NewStore(options)
		check(err)
		options = bbolt.DefaultOptions
		options.Path = db_prefix + "foreignIDs"
		options.BucketName = "foreignIDs"
		s.ForeignIDs, err = bbolt.NewStore(options)
		check(err)
		options = bbolt.DefaultOptions
		options.Path = db_prefix + "sessionTokens"
		options.BucketName = "sessionTokens"
		s.SessionTokens, err = bbolt.NewStore(options)
		check(err)
	*/

	ext := "json"
	s.Users, err = file.NewStore(file.Options{Directory: db_prefix + "users", FilenameExtension: &ext, Codec: encoding.JSON})
	s.ByNames["users"] = s.Users

	check(err)
	s.ForeignIDs, err = file.NewStore(file.Options{Directory: db_prefix + "foreignIDs", FilenameExtension: &ext, Codec: encoding.JSON})
	s.ByNames["foreignIDs"] = s.ForeignIDs
	check(err)
	s.SessionTokens, err = file.NewStore(file.Options{Directory: db_prefix + "sessionTokens", FilenameExtension: &ext, Codec: encoding.JSON})
	s.ByNames["sessionTokens"] = s.SessionTokens
	check(err)
	s.UserNames, err = file.NewStore(file.Options{Directory: db_prefix + "userNames", FilenameExtension: &ext, Codec: encoding.JSON})
	s.ByNames["userNames"] = s.UserNames
	check(err)

	/*
		s.db.Update(func(tx *bolt.Tx) error {
			_, err := tx.CreateBucketIfNotExists([]byte("users"))
			check(err)
			return nil
		})
		s.db.Update(func(tx *bolt.Tx) error {
			_, err := tx.CreateBucketIfNotExists([]byte("foreignIDs"))
			check(err)
			return nil
		})
		s.db.Update(func(tx *bolt.Tx) error {
			_, err := tx.CreateBucketIfNotExists([]byte("sessionTokens"))
			check(err)
			return nil
		})
	*/

	shutdownFunc = func() {
		defer s.Users.Close()

		defer s.ForeignIDs.Close()

		defer s.SessionTokens.Close()
	}
	return s, err, shutdownFunc
}

// Wrap basic hash functions:  exists/put/get
//
// To switch to another keyval store, e.g. AWS, we just replace the API calls here
func (s *authDB) Exists(bucket, key string) bool {
	var data []byte
	found, err := s.ByNames[bucket].Get(key, &data)
	check(err)
	return found
}

// Wrap basic hash functions:  exists/put/get
//
// To switch to another keyval store, e.g. AWS, we just replace the API calls here
func (s *authDB) Put(bucket, key string, val []byte) error {
	err := s.ByNames[bucket].Set(key, val)
	check(err)
	return err
}

// Wrap basic hash functions:  exists/put/get
//
// To switch to another keyval store, e.g. AWS, we just replace the API calls here
func (s *authDB) Get(bucket, key string) (data []byte, err error) {
	found, err := s.ByNames[bucket].Get(key, &data)
	if !found {
		return nil, err
	}
	return data, err
}
