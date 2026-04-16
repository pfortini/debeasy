package store

import "golang.org/x/crypto/bcrypt"

// Drop password hashing to the fastest legal cost during tests — cost 12 adds
// ~300ms per hash under -race, which dominates runtime when every test seeds
// an admin user and logs in.
func init() { BcryptCost = bcrypt.MinCost }
