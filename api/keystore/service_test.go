// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keystore

import (
	"bytes"
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/version"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var (
	// strongPassword defines a password used for the following tests that
	// scores high enough to pass the password strength scoring system
	strongPassword = "N_+=_jJ;^(<;{4,:*m6CET}'&N;83FYK.wtNpwp-Jt" // #nosec G101
)

func TestServiceListNoUsers(t *testing.T) {
	ks, err := CreateTestKeystore()
	if err != nil {
		t.Fatal(err)
	}

	reply := ListUsersReply{}
	if err := ks.ListUsers(nil, nil, &reply); err != nil {
		t.Fatal(err)
	}
	if len(reply.Users) != 0 {
		t.Fatalf("No users should have been created yet")
	}
}

func TestServiceCreateUser(t *testing.T) {
	ks, err := CreateTestKeystore()
	if err != nil {
		t.Fatal(err)
	}

	{
		reply := api.SuccessResponse{}
		if err := ks.CreateUser(nil, &api.UserPass{
			Username: "bob",
			Password: strongPassword,
		}, &reply); err != nil {
			t.Fatal(err)
		}
		if !reply.Success {
			t.Fatalf("User should have been created successfully")
		}
	}

	{
		reply := ListUsersReply{}
		if err := ks.ListUsers(nil, nil, &reply); err != nil {
			t.Fatal(err)
		}
		if len(reply.Users) != 1 {
			t.Fatalf("One user should have been created")
		}
		if user := reply.Users[0]; user != "bob" {
			t.Fatalf("'bob' should have been a user that was created")
		}
	}
}

// genStr returns a string of given length
func genStr(n int) string {
	b := make([]byte, n)
	rand.Read(b) // #nosec G404
	return fmt.Sprintf("%x", b)[:n]
}

// TestServiceCreateUserArgsCheck generates excessively long usernames or
// passwords to assure the sanity checks on string length are not exceeded
func TestServiceCreateUserArgsCheck(t *testing.T) {
	ks, err := CreateTestKeystore()
	if err != nil {
		t.Fatal(err)
	}

	{
		reply := api.SuccessResponse{}
		err := ks.CreateUser(nil, &api.UserPass{
			Username: genStr(maxUserLen + 1),
			Password: strongPassword,
		}, &reply)

		if reply.Success || err != errUserMaxLength {
			t.Fatal("User was created when it should have been rejected due to too long a Username, err =", err)
		}
	}

	{
		reply := api.SuccessResponse{}
		err := ks.CreateUser(nil, &api.UserPass{
			Username: "shortuser",
			Password: genStr(maxUserLen + 1),
		}, &reply)

		if reply.Success || err == nil {
			t.Fatal("User was created when it should have been rejected due to too long a Password, err =", err)
		}
	}

	{
		reply := ListUsersReply{}
		if err := ks.ListUsers(nil, nil, &reply); err != nil {
			t.Fatal(err)
		}

		if len(reply.Users) > 0 {
			t.Fatalf("A user exists when there should be none")
		}
	}
}

// TestServiceCreateUserWeakPassword tests creating a new user with a weak
// password to ensure the password strength check is working
func TestServiceCreateUserWeakPassword(t *testing.T) {
	ks, err := CreateTestKeystore()
	if err != nil {
		t.Fatal(err)
	}

	{
		reply := api.SuccessResponse{}
		err := ks.CreateUser(nil, &api.UserPass{
			Username: "bob",
			Password: "weak",
		}, &reply)

		if err == nil {
			t.Error("Expected error when testing weak password")
		}

		if reply.Success {
			t.Fatal("User was created when it should have been rejected due to weak password")
		}
	}
}

func TestServiceCreateDuplicate(t *testing.T) {
	ks, err := CreateTestKeystore()
	if err != nil {
		t.Fatal(err)
	}

	{
		reply := api.SuccessResponse{}
		if err := ks.CreateUser(nil, &api.UserPass{
			Username: "bob",
			Password: strongPassword,
		}, &reply); err != nil {
			t.Fatal(err)
		}
		if !reply.Success {
			t.Fatalf("User should have been created successfully")
		}
	}

	{
		reply := api.SuccessResponse{}
		if err := ks.CreateUser(nil, &api.UserPass{
			Username: "bob",
			Password: strongPassword,
		}, &reply); err == nil {
			t.Fatalf("Should have errored due to the username already existing")
		}
	}
}

func TestServiceCreateUserNoName(t *testing.T) {
	ks, err := CreateTestKeystore()
	if err != nil {
		t.Fatal(err)
	}

	reply := api.SuccessResponse{}
	if err := ks.CreateUser(nil, &api.UserPass{
		Password: strongPassword,
	}, &reply); err == nil {
		t.Fatalf("Shouldn't have allowed empty username")
	}
}

func TestServiceUseBlockchainDB(t *testing.T) {
	ks, err := CreateTestKeystore()
	if err != nil {
		t.Fatal(err)
	}

	{
		reply := api.SuccessResponse{}
		if err := ks.CreateUser(nil, &api.UserPass{
			Username: "bob",
			Password: strongPassword,
		}, &reply); err != nil {
			t.Fatal(err)
		}
		if !reply.Success {
			t.Fatalf("User should have been created successfully")
		}
	}

	{
		db, err := ks.GetDatabase(ids.Empty, "bob", strongPassword)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Put([]byte("hello"), []byte("world")); err != nil {
			t.Fatal(err)
		}
	}

	{
		db, err := ks.GetDatabase(ids.Empty, "bob", strongPassword)
		if err != nil {
			t.Fatal(err)
		}
		if val, err := db.Get([]byte("hello")); err != nil {
			t.Fatal(err)
		} else if !bytes.Equal(val, []byte("world")) {
			t.Fatalf("Should have read '%s' from the db", "world")
		}
	}
}

func TestServiceExportImport(t *testing.T) {
	encodings := []formatting.Encoding{formatting.Hex, formatting.CB58}
	for _, encoding := range encodings {
		ks, err := CreateTestKeystore()
		if err != nil {
			t.Fatal(err)
		}

		{
			reply := api.SuccessResponse{}
			if err := ks.CreateUser(nil, &api.UserPass{
				Username: "bob",
				Password: strongPassword,
			}, &reply); err != nil {
				t.Fatal(err)
			}
			if !reply.Success {
				t.Fatalf("User should have been created successfully")
			}
		}

		{
			db, err := ks.GetDatabase(ids.Empty, "bob", strongPassword)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Put([]byte("hello"), []byte("world")); err != nil {
				t.Fatal(err)
			}
		}

		exportArgs := ExportUserArgs{
			UserPass: api.UserPass{
				Username: "bob",
				Password: strongPassword,
			},
			Encoding: encoding,
		}
		exportReply := ExportUserReply{}
		if err := ks.ExportUser(nil, &exportArgs, &exportReply); err != nil {
			t.Fatal(err)
		}

		newKS, err := CreateTestKeystore()
		if err != nil {
			t.Fatal(err)
		}

		{
			reply := api.SuccessResponse{}
			if err := newKS.ImportUser(nil, &ImportUserArgs{
				UserPass: api.UserPass{
					Username: "bob",
					Password: "",
				},
				User: exportReply.User,
			}, &reply); err == nil {
				t.Fatal("Should have errored due to incorrect password")
			}
		}

		{
			reply := api.SuccessResponse{}
			if err := newKS.ImportUser(nil, &ImportUserArgs{
				UserPass: api.UserPass{
					Username: "",
					Password: "strongPassword",
				},
				User: exportReply.User,
			}, &reply); err == nil {
				t.Fatal("Should have errored due to empty username")
			}
		}

		{
			reply := api.SuccessResponse{}
			if err := newKS.ImportUser(nil, &ImportUserArgs{
				UserPass: api.UserPass{
					Username: "bob",
					Password: strongPassword,
				},
				User:     exportReply.User,
				Encoding: encoding,
			}, &reply); err != nil {
				t.Fatal(err)
			}
			if !reply.Success {
				t.Fatalf("User should have been imported successfully")
			}
		}

		{
			db, err := newKS.GetDatabase(ids.Empty, "bob", strongPassword)
			if err != nil {
				t.Fatal(err)
			}
			if val, err := db.Get([]byte("hello")); err != nil {
				t.Fatal(err)
			} else if !bytes.Equal(val, []byte("world")) {
				t.Fatalf("Should have read '%s' from the db", "world")
			}
		}
	}
}

func TestServiceDeleteUser(t *testing.T) {
	testUser := "testUser"
	password := "passwTest@fake01ord"
	tests := []struct {
		desc      string
		setup     func(ks *Keystore) error
		request   *api.UserPass
		want      *api.SuccessResponse
		wantError bool
	}{{
		desc:      "empty user name case",
		request:   &api.UserPass{},
		wantError: true,
	}, {
		desc:      "user not exists case",
		request:   &api.UserPass{Username: "dummy"},
		wantError: true,
	}, {
		desc:      "user exists and invalid password case",
		request:   &api.UserPass{Username: testUser, Password: "password"},
		wantError: true,
	}, {
		desc: "user exists and valid password case",
		setup: func(ks *Keystore) error {
			return ks.CreateUser(nil, &api.UserPass{Username: testUser, Password: password}, &api.SuccessResponse{})
		},
		request: &api.UserPass{Username: testUser, Password: password},
		want:    &api.SuccessResponse{Success: true},
	}, {
		desc: "delete a user, imported from import api case",
		setup: func(ks *Keystore) error {

			reply := api.SuccessResponse{}
			if err := ks.CreateUser(nil, &api.UserPass{Username: testUser, Password: password}, &reply); err != nil {
				return err
			}

			// created data in bob db
			db, err := ks.GetDatabase(ids.Empty, testUser, password)
			if err != nil {
				return err
			}
			if err := db.Put([]byte("hello"), []byte("world")); err != nil {
				return err
			}

			return nil
		},
		request: &api.UserPass{Username: testUser, Password: password},
		want:    &api.SuccessResponse{Success: true},
	}}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			ks, err := CreateTestKeystore()
			if err != nil {
				t.Fatal(err)
			}

			if tt.setup != nil {
				if err := tt.setup(ks); err != nil {
					t.Fatalf("failed to create user setup in keystore: %v", err)
				}
			}
			got := &api.SuccessResponse{}
			err = ks.DeleteUser(nil, tt.request, got)
			if (err != nil) != tt.wantError {
				t.Fatalf("DeleteUser() failed: error %v, wantError %v", err, tt.wantError)
			}

			if !tt.wantError && !reflect.DeepEqual(tt.want, got) {
				t.Fatalf("DeleteUser() failed: got %v, want %v", got, tt.want)
			}

			if err == nil && got.Success { // delete is successful
				if _, ok := ks.users[testUser]; ok {
					t.Fatalf("DeleteUser() failed: expected the user %s should be delete from users map", testUser)
				}

				// deleted user details should be available to create user again.
				if err = ks.CreateUser(nil, &api.UserPass{Username: testUser, Password: password}, &api.SuccessResponse{}); err != nil {
					t.Fatalf("failed to create user: %v", err)
				}
			}
		})
	}
}

func TestMigrateKeystoreUser(t *testing.T) {
	testUser := "testUser"
	password := "passwTest@fake01ord"
	bID := ids.Empty
	currentDB := memdb.New()
	semanticDBs := []*manager.SemanticDatabase{
		{
			Database: currentDB,
			Version:  version.NewDefaultVersion(1, 0, 0),
		},
	}

	dbManager, err := manager.NewManagerFromDBs(semanticDBs)
	if err != nil {
		t.Fatal(err)
	}
	ks := &Keystore{}
	if err := ks.Initialize(logging.NoLog{}, dbManager); err != nil {
		t.Fatal(err)
	}

	if err := ks.CreateUser(nil, &api.UserPass{Username: testUser, Password: password}, &api.SuccessResponse{}); err != nil {
		t.Fatalf("Failed to create user: %s", err)
	}

	userDB, err := ks.GetDatabase(bID, testUser, password)
	if err != nil {
		t.Fatalf("Failed to get user database: %s", err)
	}
	k1 := []byte("Old")
	v1 := []byte("Dominion")
	if err := userDB.Put(k1, v1); err != nil {
		t.Fatalf("Failed to put value in userDB: %s", err)
	}
	k2 := []byte("Luke")
	v2 := []byte("Bryan")
	if err := userDB.Put(k2, v2); err != nil {
		t.Fatalf("Failed to put value in userDB: %s", err)
	}

	semanticDBs = append(semanticDBs, &manager.SemanticDatabase{
		Database: memdb.New(),
		Version:  version.NewDefaultVersion(1, 1, 0),
	})
	upgradedDBManager, err := manager.NewManagerFromDBs(semanticDBs)
	if err != nil {
		t.Fatal(err)
	}

	ksUpgraded := &Keystore{}
	if err := ksUpgraded.Initialize(logging.NoLog{}, upgradedDBManager); err != nil {
		t.Fatal(err)
	}

	reply := &ListUsersReply{}
	if err := ksUpgraded.ListUsers(nil, nil, reply); err != nil {
		t.Fatalf("Failed to list users: %s", err)
	}

	if len(reply.Users) != 1 {
		t.Fatalf("Exepcted 1 user, but found: %d", len(reply.Users))
	}

	if reply.Users[0] != testUser {
		t.Fatalf("Expected first user to be %s, but found %s", testUser, reply.Users[0])
	}

	userDB, err = ksUpgraded.GetDatabase(bID, testUser, password)
	if err != nil {
		t.Fatalf("Failed to get user database from upgraded DB: %s", err)
	}

	value, err := userDB.Get(k1)
	if err != nil {
		t.Fatalf("Failed to get value from userDB: %s", err)
	}
	if !bytes.Equal(value, v1) {
		t.Fatalf("Expected value: %s, but found %s", value, v1)
	}

	value, err = userDB.Get(k2)
	if err != nil {
		t.Fatalf("Failed to get value from userDB: %s", err)
	}
	if !bytes.Equal(value, v2) {
		t.Fatalf("Expected value: %s, but found %s", value, v2)
	}

	semanticDBs = append(semanticDBs, &manager.SemanticDatabase{
		Database: memdb.New(),
		Version:  version.NewDefaultVersion(2, 1, 0),
	})
	secondUpgradedDBManager, err := manager.NewManagerFromDBs(semanticDBs)
	if err != nil {
		t.Fatal(err)
	}

	ksSecondUpgrade := &Keystore{}
	if err := ksSecondUpgrade.Initialize(logging.NoLog{}, secondUpgradedDBManager); err != nil {
		t.Fatal(err)
	}

	reply = &ListUsersReply{}
	if err := ksSecondUpgrade.ListUsers(nil, nil, reply); err != nil {
		t.Fatalf("Failed to list users: %s", err)
	}

	if len(reply.Users) != 1 {
		t.Fatalf("Exepcted 1 user, but found: %d", len(reply.Users))
	}

	if reply.Users[0] != testUser {
		t.Fatalf("Expected first user to be %s, but found %s", testUser, reply.Users[0])
	}

	userDB, err = ksSecondUpgrade.GetDatabase(bID, testUser, password)
	if err != nil {
		t.Fatalf("Failed to get user database from upgraded DB: %s", err)
	}

	value, err = userDB.Get(k1)
	if err != nil {
		t.Fatalf("Failed to get value from userDB: %s", err)
	}
	if !bytes.Equal(value, v1) {
		t.Fatalf("Expected value: %s, but found %s", value, v1)
	}

	value, err = userDB.Get(k2)
	if err != nil {
		t.Fatalf("Failed to get value from userDB: %s", err)
	}
	if !bytes.Equal(value, v2) {
		t.Fatalf("Expected value: %s, but found %s", value, v2)
	}
}
