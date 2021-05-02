package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"

	"github.com/rusq/chromedl"
)

const rbnzRates = "https://www.rbnz.govt.nz/-/media/ReserveBank/Files/Statistics/tables/b1/hb1-daily.xlsx?revision=5fa61401-a877-4607-b7ae-2e060c09935d"

func main() {
	r, err := chromedl.Get(context.Background(), rbnzRates)
	if err != nil {
		log.Fatal(err)
	}
	data, err := ioutil.ReadAll(r)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("file size > 0: %v", len(data))
	fmt.Printf("file signature: %s", string(data[0:2]))
}
