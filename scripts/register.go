package main

import "log"

func main() {
    InitDB()  // Or run init_db.go first
    err := RegisterCompany("946-1", "Bryan Fire & Safety", "admin", "10200mille")
    if err != nil {
        log.Fatal(err)
    }
    log.Println("Company registered.")
}