package main

import "errors"

type Args struct {
	A, B int
}

type Quotient struct {
	Quo, Rem int
}

type Shop int

func (t *Shop) Multiply(args *Args, reply *int) error {
	*reply = args.A * args.B
	return nil
}

func (t *Shop) Divide(args *Args, quo *Quotient) error {
	if args.B == 0 {
		return errors.New("divide by zero")
	}
	quo.Quo = args.A / args.B
	quo.Rem = args.A % args.B
	return nil
}
