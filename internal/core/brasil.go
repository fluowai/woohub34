package core

import (
	"fmt"
	"regexp"
	"strings"

	"go.mau.fi/whatsmeow/types"
)

var apenasDigitosRegex = regexp.MustCompile(`\D`)

// NormalizarNumeroBrasil limpa caracteres especiais, aplica DDI 55 se ausente e valida formato BR
func NormalizarNumeroBrasil(numero string) (string, error) {
	// Se for JID de grupo ou canal, retorna direto
	if strings.Contains(numero, "@g.us") || strings.Contains(numero, "@newsletter") {
		return numero, nil
	}

	digitos := apenasDigitosRegex.ReplaceAllString(numero, "")

	// Se for vazio
	if len(digitos) < 8 {
		return "", fmt.Errorf("número de telefone muito curto ou inválido: %s", numero)
	}

	// Se não começar com 55 (DDI Brasil), adiciona se tiver tamanho compatível com DDD + Telefone (10 ou 11 dígitos)
	if !strings.HasPrefix(digitos, "55") && (len(digitos) == 10 || len(digitos) == 11) {
		digitos = "55" + digitos
	}

	// Validação básica para números com DDI Brasil (55)
	if strings.HasPrefix(digitos, "55") {
		// DDI (2) + DDD (2) + Telefone (8 ou 9) = 12 ou 13 dígitos
		if len(digitos) != 12 && len(digitos) != 13 {
			return "", fmt.Errorf("número brasileiro inválido (deve conter DDI 55 + DDD + 8 ou 9 dígitos): %s", numero)
		}
	}

	return digitos, nil
}

// ObterJIDPadrao converte um número limpo em types.JID do WhatsApp
func ObterJIDPadrao(numero string) (types.JID, error) {
	if strings.Contains(numero, "@") {
		return types.ParseJID(numero)
	}

	numeroLimpo, err := NormalizarNumeroBrasil(numero)
	if err != nil {
		return types.EmptyJID, err
	}

	return types.NewJID(numeroLimpo, types.DefaultUserServer), nil
}

// AlternarNonoDigitoBrasil alterna entre o formato com e sem o 9º dígito para fallback no WhatsApp
// Ex: 5511987654321 -> 551187654321 e vice-versa
func AlternarNonoDigitoBrasil(numero string) (string, bool) {
	digitos := apenasDigitosRegex.ReplaceAllString(numero, "")

	// Apenas para Brasil (começando com 55)
	if !strings.HasPrefix(digitos, "55") {
		return numero, false
	}

	// Caso com 9º dígito: 55 (DDI) + XX (DDD) + 9 (Nono Dígito) + 8 dígitos = 13 caracteres
	if len(digitos) == 13 && digitos[4] == '9' {
		// Remove o 9 da posição 4 (índice 4)
		comOito := digitos[:4] + digitos[5:]
		return comOito, true
	}

	// Caso sem 9º dígito: 55 (DDI) + XX (DDD) + 8 dígitos = 12 caracteres
	if len(digitos) == 12 {
		// Insere o 9 logo após o DDD (na posição 4)
		comNove := digitos[:4] + "9" + digitos[4:]
		return comNove, true
	}

	return numero, false
}

// FormatarNumeroBrasilVisual retorna uma apresentação bonita para UI: +55 (11) 98765-4321
func FormatarNumeroBrasilVisual(numero string) string {
	digitos := apenasDigitosRegex.ReplaceAllString(numero, "")

	if strings.HasPrefix(digitos, "55") && len(digitos) == 13 {
		// +55 (DD) 9XXXX-XXXX
		return fmt.Sprintf("+%s (%s) %s-%s", digitos[0:2], digitos[2:4], digitos[4:9], digitos[9:13])
	} else if strings.HasPrefix(digitos, "55") && len(digitos) == 12 {
		// +55 (DD) XXXX-XXXX
		return fmt.Sprintf("+%s (%s) %s-%s", digitos[0:2], digitos[2:4], digitos[4:8], digitos[8:12])
	} else if len(digitos) == 11 {
		// (DD) 9XXXX-XXXX
		return fmt.Sprintf("(%s) %s-%s", digitos[0:2], digitos[2:7], digitos[7:11])
	}

	return numero
}
