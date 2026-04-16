package server

import (
	"golang.org/x/crypto/bcrypt"

	"github.com/pfortini/debeasy/internal/store"
)

func init() { store.BcryptCost = bcrypt.MinCost }
