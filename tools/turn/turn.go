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
    publicIP := flag.String("public-ip", "", "Public IP address of the TURN server")
    port := flag.Int("port", 3478, "Port to listen on (default 3478)")
    users := flag.String("users", "", "Comma-separated list of user=pass credentials")
    realm := flag.String("realm", "v.akhil.sh", "Authentication realm")
    flag.Parse()

    if len(*publicIP) == 0 || net.ParseIP(*publicIP) == nil {
        log.Fatalf("Valid --public-ip is required")
    }

    if len(*users) == 0 {
        log.Fatalf("--users is required (format: user=pass,user=pass)")
    }

    udpListener, err := net.ListenPacket("udp4", "0.0.0.0:"+strconv.Itoa(*port))
    if err != nil {
        log.Panicf("Failed to create TURN server listener: %s", err)
    }

    usersMap := map[string][]byte{}
    for _, kv := range regexp.MustCompile(`(\w+)=(\w+)`).FindAllStringSubmatch(*users, -1) {
        usersMap[kv[1]] = turn.GenerateAuthKey(kv[1], *realm, kv[2])
    }

    s, err := turn.NewServer(turn.ServerConfig{
        Realm: *realm,
        AuthHandler: func(username, realm string, srcAddr net.Addr) ([]byte, bool) {
            if key, ok := usersMap[username]; ok {
                return key, true
            }
            return nil, false
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
        log.Panic(err)
    }

    log.Printf("TURN server started on %s:%d with realm '%s'", *publicIP, *port, *realm)

    sigs := make(chan os.Signal, 1)
    signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
    <-sigs

    if err = s.Close(); err != nil {
        log.Panic(err)
    }
}
