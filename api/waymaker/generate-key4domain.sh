#!/bin/bash
DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"

DOMAIN=$1

if [ -z "$DOMAIN" ]
then
	echo -e "\033[41mcommand is not correct\033[m"
	echo "$0 DOMAIN"
	exit 1
fi

KEY_PATH=/tmp/$DOMAIN-key.pem
CERT_PATH=/tmp/$DOMAIN-cert.pem
TPL_PATH=/tmp/$DOMAIN.tmpl

CA_CN="His certificate!"
CA_ORG="His CA!"
CA_DAYS=99999
SRV_ORG="yes. corp.?"
SRV_DAYS=99999
SRV_CN="$DOMAIN"

certtool --generate-privkey --outfile $KEY_PATH
cat > $TPL_PATH <<-EOSRV
cn = "$SRV_CN"
organization = "$SRV_ORG"
expiration_days = $SRV_DAYS
signing_key
encryption_key
tls_www_server
dns_name = "$DOMAIN"
EOSRV

certtool --generate-certificate --load-privkey $KEY_PATH --load-ca-certificate "$DIR/ca_cert.pem" --load-ca-privkey "$DIR/ca_key.pem" --template $TPL_PATH --outfile $CERT_PATH

echo $KEY_PATH
echo $CERT_PATH