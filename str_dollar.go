package main

import "strings"

var strDollar TwoBitSet

const (
	dollarUnseen DollarMemoState = iota
	dollarAbsent
	dollarPresent
)

type DollarMemoState uint8

func strHasDollar(id ANY) bool {
	if cell := DollarMemoState(strDollar.get(uint32(id))); cell != dollarUnseen {
		return cell == dollarPresent
	}

	yes := strings.Contains(id.string(), "$")

	if yes {
		strDollar.set(uint32(id), uint8(dollarPresent))
	} else {
		strDollar.set(uint32(id), uint8(dollarAbsent))
	}

	return yes
}
