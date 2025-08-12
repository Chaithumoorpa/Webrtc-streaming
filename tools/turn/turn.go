package main

import (
    "flag"
    "log"
    "net"
    "os"
    "os/signal"
    "regexp"
    "strconv"
    "syscall"

    "github.com/pion/turn/v2"
)

func main() {
    // Parse command-line flags
    publicIP := flag.String("public-ip", "", "Public IP address of the TURN server")
    port := flag.Int("port", 3478, "Port to listen on (default 3478)")
    users := flag.String("users", "", "Comma-separated list of user=pass credentials")
    realm := flag.String("realm", "v.akhil.sh", "Authentication realm")
    flag.Parse()

    // Validate input
    if ip := net.ParseIP(*publicIP); ip == nil {
        log.Fatalf("Valid --public-ip is required")
    }
    if len(*users) == 0 {
        log.Fatalf("--users is required (format: user=pass,user=pass)")
    }

    // Create UDP listener
    addr := "0.0.0.0:" + strconv.Itoa(*port)
    udpListener, err := net.ListenPacket("udp4", addr)
    if err != nil {
        log.Panicf("Failed to create TURN server listener: %v", err)
    }

    // Parse user credentials
    usersMap := parseUsers(*users, *realm)

    // Start TURN server
    server, err := turn.NewServer(turn.ServerConfig{
        Realm: *realm,
        AuthHandler: func(username, realm string, srcAddr net.Addr) ([]byte, bool) {
            key, ok := usersMap[username]
            return key, ok
        },
        PacketConnConfigs: []turn.PacketConnConfig{
            {
                PacketConn: udpListener,
                RelayAddressGenerator: &turn.RelayAddressGeneratorPortRange{
                    RelayAddress: net.ParseIP(*publicIP),
                    Address:      "0.0.0.0",
                    MinPort:      50000,
                    MaxPort:      55000,
                },
            },
        },
    })
    if err != nil {
        log.Panicf("Failed to start TURN server: %v", err)
    }

    log.Printf("TURN server started on %s:%d with realm '%s'", *publicIP, *port, *realm)

    // Graceful shutdown
    waitForShutdown()
    if err := server.Close(); err != nil {
        log.Panicf("Failed to shut down TURN server: %v", err)
    }
}

// Parses user credentials from flag input
func parseUsers(input, realm string) map[string][]byte {
    usersMap := make(map[string][]byte)
    re := regexp.MustCompile(`(\w+)=(\w+)`)
    for _, match := range re.FindAllStringSubmatch(input, -1) {
        username, password := match[1], match[2]
        usersMap[username] = turn.GenerateAuthKey(username, realm, password)
    }
    return usersMap
}

// Waits for termination signal
func waitForShutdown() {
    sigs := make(chan os.Signal, 1)
    signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
    <-sigs
}

