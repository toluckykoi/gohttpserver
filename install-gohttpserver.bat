@echo off
setlocal EnableDelayedExpansion

REM gohttpserver installer for Windows CMD
REM Author: luckykoi
REM Version: 1.0

REM Global variables
set "InstallDir=%ProgramFiles%\gohttpserver"
set "DataDir=%InstallDir%\data"
set "ServiceName=gohttpserver"
set "BinaryPath=%InstallDir%\gohttpserver.exe"
set "DbFile=%InstallDir%\gohttpserver.db"
set "LauncherPath=%InstallDir%\start-gohttpserver.bat"
set "LogFile=%InstallDir%\gohttpserver.log"
set "DefaultPort=8000"
set "ListenPort=0"
set "Repo=toluckykoi/gohttpserver"
set "Channel=stable"
set "Mirror=github"
set "EnableUpload=false"
set "EnableDelete=false"
set "EnableEdit=false"
set "EnableLogin=false"
set "RootDir="

REM Help
if /i "%~1"=="-h" goto ShowHelp
if /i "%~1"=="--help" goto ShowHelp
if /i "%~1"=="/?" goto ShowHelp
goto MainStart

:ShowHelp
echo gohttpserver installer for Windows CMD
echo.
echo Usage: install-gohttpserver.bat
echo.
echo Features:
echo   - Install/Upgrade/Uninstall gohttpserver
echo   - Auto-detect GitHub/Gitee mirror
echo   - Install dir: %ProgramFiles%\gohttpserver
echo.
echo Options: -h, --help, /?  Show this help
exit /b 0

:MainStart
call :CheckAdmin
if errorlevel 1 (
    echo [ERROR] Please run as Administrator
    exit /b 1
)

call :SelectMirror

:MainMenu
cls
call :ShowBanner
echo.
echo ==============================================================
echo   GoHTTPServer Installer
echo ==============================================================
echo Select operation:
echo   1) Install   2) Upgrade   3) Uninstall   4) Status
echo   5) Logs      6) Restart   7) Stop        8) Exit
echo.
set /p choice=Enter option: 

if "%choice%"=="1" goto InstallBinary
if "%choice%"=="2" goto UpgradeBinary
if "%choice%"=="3" goto UninstallBinary
if "%choice%"=="4" goto ShowStatus
if "%choice%"=="5" goto ShowLogs
if "%choice%"=="6" goto RestartService
if "%choice%"=="7" goto StopService
if "%choice%"=="8" exit /b 0

echo [WARN] Invalid option
timeout /t 1 >nul
goto MainMenu

:WriteInfo
echo [INFO]  %~1
exit /b 0

:WriteSuccess
echo [OK]    %~1
exit /b 0

:WriteWarn
echo [WARN]  %~1
exit /b 0

:WriteErr
echo [ERROR] %~1
exit /b 0

:WriteStep
echo [STEP]  %~1
exit /b 0

:CheckAdmin
net session >nul 2>&1
exit /b %errorlevel%

:GetArch
REM 返回产物文件名中的架构标识（与 build.sh 的产物命名一致）
set "Arch=x64"
if "%PROCESSOR_ARCHITECTURE%"=="ARM64" set "Arch=arm64"
if defined PROCESSOR_ARCHITEW6432 (
    if "%PROCESSOR_ARCHITEW6432%"=="ARM64" set "Arch=arm64"
)
exit /b 0

:SelectMirror
call :WriteStep "Detecting network..."
curl -s -o nul -m 5 https://github.com 2>nul
if errorlevel 1 (
    set "Mirror=gitee"
    call :WriteInfo "Using Gitee mirror"
) else (
    set "Mirror=github"
    call :WriteInfo "Using GitHub mirror"
)
exit /b 0

:TestInstalled
if exist "%BinaryPath%" exit /b 0
exit /b 1

:TestTaskExists
schtasks /Query /TN "%ServiceName%" >nul 2>&1
exit /b %errorlevel%

:TestProcessRunning
tasklist /FI "IMAGENAME eq gohttpserver.exe" 2>nul | find /i "gohttpserver.exe" >nul
exit /b %errorlevel%

:ShowBanner
cls
set "MirrorDisplay=GitHub"
if "%Mirror%"=="gitee" set "MirrorDisplay=Gitee"
echo ==============================================================
echo         GoHTTPServer Installer (Windows CMD)
echo    https://github.com/toluckykoi/gohttpserver
echo    Mirror: !MirrorDisplay!
echo ==============================================================
exit /b 0

:SelectChannel
echo.
echo ==============================================================
echo   Select Release Channel
echo ==============================================================
echo   1) Stable (recommended)
echo   2) Beta (latest features)
echo.
set /p channelChoice=Enter option:
if "!channelChoice!"=="2" (
    set "Channel=beta"
) else (
    set "Channel=stable"
)
call :WriteInfo "Channel: !Channel!"
exit /b 0

:ConfirmYesNo
set /p "confirm=%~1 (Y/n): "
if /i "!confirm!"=="n" exit /b 1
exit /b 0

:ShowMessageBox
echo.
echo ==============================================================
echo   %~1
echo ==============================================================
echo %~2
echo ==============================================================
pause
exit /b 0

:SelectFeatures
echo.
call :ConfirmYesNo "Enable login authentication?"
if not errorlevel 1 (
    set "EnableLogin=true"
    set "EnableUpload=true"
    set "EnableDelete=true"
    set "EnableEdit=true"
    call :WriteInfo "Login enabled, auto-enabled upload/delete/edit"
) else (
    call :ConfirmYesNo "Enable file upload?"
    if not errorlevel 1 set "EnableUpload=true"
    call :ConfirmYesNo "Enable file delete?"
    if not errorlevel 1 set "EnableDelete=true"
    call :ConfirmYesNo "Enable file edit?"
    if not errorlevel 1 set "EnableEdit=true"
)
set /p "RootDir=Enter data folder path [default: %DataDir%]: "
if not defined RootDir set "RootDir=%DataDir%"
if not exist "!RootDir!" mkdir "!RootDir!"
call :WriteInfo "Data folder: !RootDir!"
exit /b 0

:GetDownloadUrl
call :GetArch
set "Tag="
set "DownloadUrl="
if "%Mirror%"=="gitee" (
    if "!Channel!"=="beta" (
        set "ApiUrl=https://gitee.com/api/v5/repos/%Repo%/releases?page=1&per_page=30"
    ) else (
        set "ApiUrl=https://gitee.com/api/v5/repos/%Repo%/releases/latest"
    )
) else (
    if "!Channel!"=="beta" (
        set "ApiUrl=https://api.github.com/repos/%Repo%/releases?per_page=30"
    ) else (
        set "ApiUrl=https://api.github.com/repos/%Repo%/releases/latest"
    )
)
set "TmpFile=%TEMP%\gohttpserver_api_%RANDOM%.tmp"
curl -s -H "User-Agent: gohttpserver-installer" -o "!TmpFile!" "!ApiUrl!" 2>nul
if errorlevel 1 (
    del "!TmpFile!" >nul 2>&1
    exit /b 1
)
REM Use PowerShell to parse JSON reliably
set "PsCmd=(Get-Content '%TmpFile%' -Raw | ConvertFrom-Json)"
if "!Channel!"=="beta" (
    set "PsCmd=!PsCmd! | ForEach-Object { $_.tag_name } | Where-Object { $_ -match '-beta' } | Select-Object -First 1"
) else (
    set "PsCmd=!PsCmd!.tag_name"
)
for /f "tokens=*" %%a in ('powershell -NoProfile -Command "!PsCmd!"') do (
    if not defined Tag set "Tag=%%a"
)
del "!TmpFile!" >nul 2>&1
if not defined Tag (
    call :WriteErr "Version not found"
    exit /b 1
)
REM Convert tag to the version part of the artifact name
REM (v1.2.0 -> 1.2.0, v1.3.0-beta.1 -> 1.3.0-beta.1)
REM Artifact name: gohttpserver-<version>-<os>-<arch>.exe
set "TagVersion=!Tag!"
if /i "!Tag:~0,1!"=="v" set "TagVersion=!Tag:~1!"
set "FileName=gohttpserver-!TagVersion!-windows-!Arch!.exe"
call :WriteInfo "Version: !Tag!"
call :WriteInfo "File: !FileName!"
if "%Mirror%"=="gitee" (
    set "DownloadUrl=https://gitee.com/%Repo%/releases/download/!Tag!/!FileName!"
) else (
    set "DownloadUrl=https://github.com/%Repo%/releases/download/!Tag!/!FileName!"
)
call :WriteInfo "URL: !DownloadUrl!"
exit /b 0

:GetSupportedFlags
set "SupportedFlags="
set "HelpOutput=%TEMP%\gohttpserver_help_%RANDOM%.tmp"
"%BinaryPath%" --help >"!HelpOutput!" 2>&1
findstr /i /c:"--upload" "!HelpOutput!" >nul && set "SupportedFlags=!SupportedFlags! upload"
findstr /i /c:"--delete" "!HelpOutput!" >nul && set "SupportedFlags=!SupportedFlags! delete"
findstr /i /c:"--edit" "!HelpOutput!" >nul && set "SupportedFlags=!SupportedFlags! edit"
findstr /i /c:"--login" "!HelpOutput!" >nul && set "SupportedFlags=!SupportedFlags! login"
del "!HelpOutput!" >nul 2>&1
exit /b 0

:GetExecArgs
call :GetSupportedFlags
set "ExecArgs=--port !ListenPort! --root ^"!RootDir!^""
if "!EnableUpload!"=="true" (
    echo !SupportedFlags! | findstr /i "upload" >nul
    if not errorlevel 1 (set "ExecArgs=!ExecArgs! --upload") else (call :WriteWarn "Not support --upload")
)
if "!EnableDelete!"=="true" (
    echo !SupportedFlags! | findstr /i "delete" >nul
    if not errorlevel 1 (set "ExecArgs=!ExecArgs! --delete") else (call :WriteWarn "Not support --delete")
)
if "!EnableEdit!"=="true" (
    echo !SupportedFlags! | findstr /i "edit" >nul
    if not errorlevel 1 (set "ExecArgs=!ExecArgs! --edit") else (call :WriteWarn "Not support --edit")
)
if "!EnableLogin!"=="true" (
    echo !SupportedFlags! | findstr /i "login" >nul
    if not errorlevel 1 (set "ExecArgs=!ExecArgs! --login") else (call :WriteWarn "Not support --login")
)
exit /b 0

:NewLauncher
REM Build args with relative path for launcher
set "LauncherArgs=--port !ListenPort! --root data"
if "!EnableUpload!"=="true" set "LauncherArgs=!LauncherArgs! --upload"
if "!EnableDelete!"=="true" set "LauncherArgs=!LauncherArgs! --delete"
if "!EnableEdit!"=="true" set "LauncherArgs=!LauncherArgs! --edit"
if "!EnableLogin!"=="true" set "LauncherArgs=!LauncherArgs! --login"
(
echo @echo off
echo cd /d "%%~dp0"
echo echo ============================================================
echo echo  GoHTTPServer ^(port: !ListenPort!^)
echo echo  Log: %%~dp0gohttpserver.log
echo echo ============================================================
echo.
echo REM Check for admin rights
echo net session ^>nul 2^>^&1
echo if errorlevel 1 ^(
echo     echo Requesting administrator privileges...
echo     powershell -Command "Start-Process -FilePath '%%~f0' -Verb RunAs"
echo     exit /b
echo ^)
echo.
echo gohttpserver.exe !LauncherArgs! ^>^> "%%~dp0gohttpserver.log" 2^>^&1
echo pause
) > "!LauncherPath!"
call :WriteSuccess "Launcher script created"
exit /b 0

:RegisterGohttpTask
call :WriteStep "Creating scheduled task..."
schtasks /Create /TN "%ServiceName%" /TR "\"!LauncherPath!\"" /SC ONSTART /RU SYSTEM /RL HIGHEST /F >nul 2>&1
if errorlevel 1 (
    call :WriteErr "Failed to create task"
    exit /b 1
)
call :WriteSuccess "Task created"
schtasks /Run /TN "%ServiceName%" >nul 2>&1
timeout /t 2 >nul
call :TestProcessRunning
exit /b %errorlevel%

:StopGohttp
call :TestTaskExists
if not errorlevel 1 schtasks /End /TN "%ServiceName%" >nul 2>&1
taskkill /F /IM gohttpserver.exe >nul 2>&1
timeout /t 1 >nul
exit /b 0

:ShowAccessInfo
set "IP=localhost"
for /f "tokens=2 delims=:" %%a in ('ipconfig ^| findstr /i "IPv4" ^| findstr /v "127.0.0.1"') do (
    for /f "tokens=1" %%b in ("%%a") do set "IP=%%b"
)
echo.
echo ==============================================================
echo   Installation Complete
echo ==============================================================
echo Access Info:
echo   URL: http://!IP!:!ListenPort!
echo   Data folder: !RootDir!
if "!EnableLogin!"=="true" (
    echo.
    echo Login Info:
    echo   Username: admin
    echo   Password: admin
)
echo.
echo Commands:
echo   Status: schtasks /Query /TN %ServiceName% /V /FO LIST
echo   Start: schtasks /Run /TN %ServiceName%
echo   Stop: schtasks /End /TN %ServiceName%
echo ==============================================================
pause
exit /b 0

:InstallBinary
call :WriteStep "Starting installation..."
call :TestInstalled
if not errorlevel 1 (
    call :ShowMessageBox "Info" "GoHTTPServer already installed"
    goto MainMenu
)
call :SelectChannel
:PortInput
set /p "inputPort=Enter port (1-65535) [default: %DefaultPort%]: "
if not defined inputPort set "inputPort=%DefaultPort%"
REM Validate port is numeric
echo !inputPort!| findstr /b /e /r "[0-9][0-9]*" >nul
if errorlevel 1 (
    call :WriteWarn "Invalid port"
    goto PortInput
)
if !inputPort! LSS 1 (
    call :WriteWarn "Invalid port"
    goto PortInput
)
if !inputPort! GTR 65535 (
    call :WriteWarn "Invalid port"
    goto PortInput
)
set "ListenPort=!inputPort!"
call :SelectFeatures
call :GetArch
call :WriteStep "Creating install directory..."
if not exist "%InstallDir%" mkdir "%InstallDir%"
call :GetDownloadUrl
if errorlevel 1 (
    call :ShowMessageBox "Error" "Failed to get download URL"
    goto MainMenu
)
call :WriteStep "Downloading GoHTTPServer..."
curl -L -o "%BinaryPath%" "!DownloadUrl!"
if errorlevel 1 (
    del "%BinaryPath%" >nul 2>&1
    call :ShowMessageBox "Error" "Download failed"
    goto MainMenu
)

REM Verify downloaded file is valid PE executable
set "FileSize="
for %%A in ("%BinaryPath%") do set "FileSize=%%~zA"
if !FileSize! LSS 1000000 (
    call :WriteErr "Downloaded file is too small (!FileSize! bytes)"
    call :WriteErr "This may indicate a download failure (404 page or error)"
    del "%BinaryPath%" >nul 2>&1
    call :ShowMessageBox "Error" "Invalid download - file too small. Check if the release exists."
    goto MainMenu
)

REM Check PE header (MZ signature)
set /p MZHeader=<"%BinaryPath%"
if not "!MZHeader:~0,2!"=="MZ" (
    call :WriteErr "Downloaded file is not a valid executable"
    del "%BinaryPath%" >nul 2>&1
    call :ShowMessageBox "Error" "Invalid executable file downloaded"
    goto MainMenu
)

call :WriteSuccess "Download verified (!FileSize! bytes)"
call :WriteStep "Detecting parameters..."
call :GetExecArgs
call :NewLauncher
echo.
echo ==============================================================
echo   Run Mode
echo ==============================================================
echo   1) Scheduled task (auto-start, recommended)
echo   2) Manual run only
echo.
set /p modeChoice=Enter option: 
if "!modeChoice!"=="2" (
    call :ShowMessageBox "Complete" "Manual start: !LauncherPath!"
    goto MainMenu
)
call :RegisterGohttpTask
if not errorlevel 1 (
    call :WriteSuccess "Service started"
    call :ShowAccessInfo
) else (
    call :ShowMessageBox "Error" "Service failed to start"
)
goto MainMenu

:UpgradeBinary
call :WriteStep "Upgrading GoHTTPServer..."
call :TestInstalled
if errorlevel 1 (
    call :ShowMessageBox "Error" "GoHTTPServer not installed"
    goto MainMenu
)
call :SelectChannel
call :TestTaskExists
set "HasTask=!errorlevel!"
call :StopGohttp
call :WriteStep "Backing up current version..."
for /f "tokens=1-6 delims=/-: " %%a in ("%date% %time: =0%") do set "Timestamp=%%a%%b%%c_%%d%%e%%f"
set "BackupPath=%BinaryPath%.backup.!Timestamp!"
copy "%BinaryPath%" "!BackupPath!" >nul
call :GetDownloadUrl
if errorlevel 1 (
    move "!BackupPath!" "%BinaryPath%" >nul
    if "!HasTask!"=="0" schtasks /Run /TN "%ServiceName%" >nul 2>&1
    call :ShowMessageBox "Error" "Failed to get download URL"
    goto MainMenu
)
call :WriteStep "Downloading latest version..."
curl -L -o "%BinaryPath%" "!DownloadUrl!"
if errorlevel 1 (
    move "!BackupPath!" "%BinaryPath%" >nul
    if "!HasTask!"=="0" schtasks /Run /TN "%ServiceName%" >nul 2>&1
    call :ShowMessageBox "Error" "Download failed"
    goto MainMenu
)
if "!HasTask!"=="0" (
    schtasks /Run /TN "%ServiceName%" >nul 2>&1
    timeout /t 2 >nul
    call :TestProcessRunning
    if not errorlevel 1 (call :ShowMessageBox "Complete" "Upgrade success") else (call :ShowMessageBox "Error" "Service failed to start")
) else (
    call :ShowMessageBox "Complete" "Manual start: !LauncherPath!"
)
goto MainMenu

:UninstallBinary
call :WriteStep "Uninstalling GoHTTPServer..."
call :TestInstalled
if errorlevel 1 (
    call :ShowMessageBox "Info" "GoHTTPServer not installed"
    goto MainMenu
)
echo.
call :ConfirmYesNo "Confirm uninstall?"
if errorlevel 1 goto MainMenu
set "DeleteData=false"
call :ConfirmYesNo "Delete user data?"
if not errorlevel 1 set "DeleteData=true"
set "DeleteDb=false"
if exist "%DbFile%" (
    call :ConfirmYesNo "Delete login database?"
    if not errorlevel 1 set "DeleteDb=true"
)
call :TestTaskExists
if not errorlevel 1 (
    call :StopGohttp
    schtasks /Delete /TN "%ServiceName%" /F >nul 2>&1
) else (
    call :StopGohttp
)
del "%BinaryPath%" >nul 2>&1
del "%LauncherPath%" >nul 2>&1
del "%LogFile%" >nul 2>&1
if "!DeleteDb!"=="true" (
    del "%DbFile%" >nul 2>&1
    del "%DbFile%-shm" >nul 2>&1
    del "%DbFile%-wal" >nul 2>&1
)
if "!DeleteData!"=="true" (
    if exist "%DataDir%" rmdir /s /q "%DataDir%" >nul 2>&1
)
if "!DeleteData!"=="true" if not exist "%DbFile%" (
    rmdir /s /q "%InstallDir%" >nul 2>&1
) else (
    rmdir "%InstallDir%" >nul 2>&1
)
call :ShowMessageBox "Complete" "GoHTTPServer uninstalled"
goto MainMenu

:ShowStatus
call :TestInstalled
if errorlevel 1 (
    call :ShowMessageBox "Error" "GoHTTPServer not installed"
    goto MainMenu
)
call :TestTaskExists
if not errorlevel 1 (schtasks /Query /TN "%ServiceName%" /FO LIST /V) else (call :WriteInfo "No scheduled task")
call :TestProcessRunning
if not errorlevel 1 (call :WriteSuccess "Process running") else (call :WriteWarn "Process not running")
pause
goto MainMenu

:ShowLogs
call :TestInstalled
if errorlevel 1 (
    call :ShowMessageBox "Error" "GoHTTPServer not installed"
    goto MainMenu
)
if not exist "%LogFile%" (
    call :ShowMessageBox "Info" "No log file"
    goto MainMenu
)
call :WriteStep "View logs (Ctrl+C to exit)"
type "%LogFile%" | more
pause
goto MainMenu

:RestartService
call :TestInstalled
if errorlevel 1 (
    call :ShowMessageBox "Error" "GoHTTPServer not installed"
    goto MainMenu
)
call :WriteStep "Restarting service..."
call :StopGohttp
call :TestTaskExists
if not errorlevel 1 (
    schtasks /Run /TN "%ServiceName%" >nul 2>&1
    timeout /t 2 >nul
    call :TestProcessRunning
    if not errorlevel 1 (call :ShowMessageBox "Success" "Service restarted") else (call :ShowMessageBox "Error" "Restart failed")
) else (
    call :ShowMessageBox "Info" "Manual start: !LauncherPath!"
)
goto MainMenu

:StopService
call :TestInstalled
if errorlevel 1 (
    call :ShowMessageBox "Error" "GoHTTPServer not installed"
    goto MainMenu
)
call :WriteStep "Stopping service..."
call :StopGohttp
call :ShowMessageBox "Success" "Service stopped"
goto MainMenu
