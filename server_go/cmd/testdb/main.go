package main

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/glebarez/go-sqlite/compat"
)

func main() {
	dbPath := "/Users/koan/PBD_trial_electron/db/private_buddy.db"

	info, err := os.Stat(dbPath)
	if err != nil {
		fmt.Println("Stat error:", err)
		return
	}
	fmt.Printf("File exists: %v, size: %d, mode: %v\n", info.IsDir(), info.Size(), info.Mode())

	dirInfo, err := os.Stat("/Users/koan/PBD_trial_electron/db")
	if err != nil {
		fmt.Println("Dir stat error:", err)
		return
	}
	fmt.Printf("Dir mode: %v, writable: %v\n", dirInfo.Mode(), dirInfo.Mode().Perm()&0200 != 0)

	file, err := os.OpenFile(dbPath, os.O_RDWR, 0)
	if err != nil {
		fmt.Println("OpenFile error:", err)
	} else {
		fmt.Println("OpenFile succeeded!")
		file.Close()
	}

	testFile := "/Users/koan/PBD_trial_electron/db/_go_write_test"
	err = os.WriteFile(testFile, []byte("test"), 0644)
	if err != nil {
		fmt.Println("Write test file error:", err)
	} else {
		fmt.Println("Write test file succeeded!")
		os.Remove(testFile)
	}

	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_txlock=immediate"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		fmt.Println("sql.Open error:", err)
		return
	}
	defer db.Close()

	var version string
	err = db.QueryRow("SELECT sqlite_version()").Scan(&version)
	if err != nil {
		fmt.Println("Query error:", err)
	} else {
		fmt.Println("SQLite version:", version)
	}

	_, err = db.Exec("CREATE TABLE IF NOT EXISTS _test (id INTEGER)")
	if err != nil {
		fmt.Println("Write error:", err)
	} else {
		fmt.Println("Write succeeded!")
		db.Exec("DROP TABLE IF EXISTS _test")
	}

	db2, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		fmt.Println("sql.Open (plain) error:", err)
		return
	}
	defer db2.Close()

	_, err = db2.Exec("CREATE TABLE IF NOT EXISTS _test (id INTEGER)")
	if err != nil {
		fmt.Println("Write (plain) error:", err)
	} else {
		fmt.Println("Write (plain) succeeded!")
		db2.Exec("DROP TABLE IF EXISTS _test")
	}
}
