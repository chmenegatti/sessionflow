//go:build race

package service

// raceEnabled: o detector deixa as consultas ~40x mais lentas; testes de
// tempo não fazem sentido nesse modo.
const raceEnabled = true
