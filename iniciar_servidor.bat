@echo off
title WooHub API (localhost:8080)
cd /d "c:\Users\paulo\whatsmeow"
echo Encerrando instancias anteriores...
taskkill /F /IM woohub-api.exe >nul 2>&1
timeout /t 1 /nobreak >nul
echo Iniciando WooHub API em http://localhost:8080 ...
"c:\Users\paulo\whatsmeow\bin\woohub-api.exe"
pause


