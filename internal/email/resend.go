package email

import ("bytes"; "encoding/json"; "fmt"; "net/http"; "strings"; "time")

type Cliente struct { APIKey, From, FromName string }
func (c Cliente) Enviar(to, subject, html string) error {
	if strings.TrimSpace(c.APIKey)=="" { return fmt.Errorf("Resend não configurado") }
	from:=c.From; if c.FromName!="" { from=c.FromName+" <"+c.From+">" }
	b,_:=json.Marshal(map[string]interface{}{ "from":from,"to":[]string{to},"subject":subject,"html":html })
	req,err:=http.NewRequest(http.MethodPost,"https://api.resend.com/emails",bytes.NewReader(b)); if err!=nil{return err}; req.Header.Set("Authorization","Bearer "+c.APIKey); req.Header.Set("Content-Type","application/json")
	resp,err:=(&http.Client{Timeout:15*time.Second}).Do(req); if err!=nil{return err}; defer resp.Body.Close(); if resp.StatusCode<200||resp.StatusCode>=300{return fmt.Errorf("Resend retornou HTTP %d",resp.StatusCode)}; return nil
}
