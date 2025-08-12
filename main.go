package main

import (
	"log"
	"webrtc-streaming/internal/server"
)

func main(){
	if err := server.Run(); err!= nil{
		log.Fatalln(err.Error())
	}
}