call %~dp0..\utils.bat

SET argC=0
for %%x in (%*) do SET /A argC+=1

IF %argC% NEQ 3 (
  ECHO "Usage: %0 [mtu] [localif] [ip/netmask]"
  exit 1
)

SET mtu=%1
SET localif=%2
SET ip=%3

:: set IP for interface
netsh interface ipv4 set address source=static name="Ethernet 2" address=%ip%
