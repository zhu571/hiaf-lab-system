package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

func main() {
	password, err := common.ReadSecret("/run/secrets/agent_password", "AGENT_PASSWORD")
	if err != nil {
		fatal(err)
	}
	db, err := common.OpenDB()
	if err != nil {
		fatal(err)
	}
	defer db.Close()

	repo := auth.NewRepository(db)
	existing, err := repo.GetByUsername("agent@system")
	if err != nil {
		fatal(err)
	}
	if existing != nil {
		if existing.Role != auth.RoleAgent {
			fatal(errors.New("agent@system exists with a non-agent role"))
		}
		fmt.Println("agent@system already exists")
		return
	}
	_, _, err = auth.NewService(repo, nil).AdminCreateUser(auth.AdminCreateUserRequest{
		Username: "agent@system", DisplayName: "System Agent", Role: auth.RoleAgent, Password: password,
	})
	if err != nil {
		fatal(err)
	}
	fmt.Println("agent@system created")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
