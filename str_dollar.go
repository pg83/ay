package main

import "strings"

var strDollar TwoBitSet

const (
	dollarUnseen DollarMemoState = iota
	dollarAbsent
	dollarPresent
	dollarDeref
)

type DollarMemoState uint8

func strHasDollar(id ANY) bool {
	if cell := DollarMemoState(strDollar.get(uint32(id))); cell != dollarUnseen {
		return cell >= dollarPresent
	}

	return strDollarFill(id) >= dollarPresent
}

func strDollarState(id ANY) DollarMemoState {
	if cell := DollarMemoState(strDollar.get(uint32(id))); cell != dollarUnseen {
		return cell
	}

	return strDollarFill(id)
}

func strDollarFill(id ANY) DollarMemoState {
	text := id.string()
	state := dollarAbsent

	if strings.Contains(text, "$") {
		state = dollarPresent

		if textDerefChanges(text) {
			state = dollarDeref
		}
	}

	strDollar.set(uint32(id), uint8(state))

	return state
}
