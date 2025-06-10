# Copyright (c) Abstract Machines
# SPDX-License-Identifier: Apache-2.0

CRT_LOCATION = certs
O = Magistrala
OU_CA = magistrala_ca
OU_CRT = magistrala_crt
EA = info@magistrala.com
CN_CA = Magistrala_Self_Signed_CA
CN_SRV = localhost
CLINET_CN = mProxy_client
CRT_FILE_NAME = client
OCSP_URL=http://localhost:8080/ocsp
OCSP_CA_URL=http://localhost:8080/ca.pem
CRL_URL=http://localhost:8080/crl.pem

define CERT_CONFIG
[ca]
subjectKeyIdentifier=hash
authorityKeyIdentifier=keyid:always,issuer:always

[req]
req_extensions = v3_req
distinguished_name = dn
prompt = no

[dn]
CN = $(COMMON_NAME)
C  = RS
ST = RS
L  = BELGRADE
O  = MAGISTRALA
OU = MAGISTRALA

[v3_req]
basicConstraints = CA:FALSE
keyUsage = nonRepudiation, digitalSignature, keyEncipherment
subjectAltName = @alt_names
authorityInfoAccess = @issuer_info
crlDistributionPoints = @crl_info

[alt_names]
DNS.1 = $(COMMON_NAME)

[issuer_info]
OCSP;URI.0 = $(OCSP_URL)
caIssuers;URI.0 = $(OCSP_CA_URL)

[crl_info]
URI.0 = $(CRL_URL)
endef

define REVOKE_CONFIG
[ca]
default_ca      = CA_default

[CA_default]
database        = $(CRT_LOCATION)/index.txt
crlnumber       = $(CRT_LOCATION)/crlnumber

default_crl_days  = 30
default_md        = sha256
policy            = policy_match

endef

define ANNOUNCE_BODY
Version $(VERSION) of $(PACKAGE_NAME) has been released.

It can be downloaded from $(DOWNLOAD_URL).

etc, etc.
endef
all: clean_certs ca server_cert client_cert client_cert_revoked client_cert_unknown

.PHONY: all clean_certs ca server_cert client_cert client_cert_revoked client_cert_unknown target

# CA name and key is "ca".
ca:
	openssl req -newkey rsa:2048 -x509 -nodes -sha512 -days 1095 \
				-keyout $(CRT_LOCATION)/ca.key -out $(CRT_LOCATION)/ca.crt -subj "/CN=$(CN_CA)/O=$(O)/OU=$(OU_CA)/emailAddress=$(EA)"

# Server cert and key name is "server".
server_cert:
	$(eval COMMON_NAME=localhost)
	$(file > $(CRT_LOCATION)/server.cnf,$(CERT_CONFIG) )

	# Create magistrala server key and CSR.
	openssl req -new -sha256 -newkey rsa:4096 -nodes \
				-keyout $(CRT_LOCATION)/server.key \
				-out $(CRT_LOCATION)/server.csr \
				-config $(CRT_LOCATION)/server.cnf \
				-extensions v3_req

	# Sign server CSR.
	openssl x509 -req -days 1000 -in $(CRT_LOCATION)/server.csr  \
				-CA $(CRT_LOCATION)/ca.crt \
				-CAkey $(CRT_LOCATION)/ca.key \
				-CAcreateserial \
				-out $(CRT_LOCATION)/server.crt \
				-extfile $(CRT_LOCATION)/server.cnf \
				-extensions v3_req

	# Remove CSR.
	rm $(CRT_LOCATION)/server.csr  $(CRT_LOCATION)/server.cnf


client_cert:
	# Create magistrala server key and CSR.
	openssl req -new -sha256 -newkey rsa:4096 -nodes -keyout $(CRT_LOCATION)/$(CRT_FILE_NAME).key \
				-out $(CRT_LOCATION)/$(CRT_FILE_NAME).csr -subj "/CN=$(CLINET_CN)/O=$(O)/OU=$(OU_CRT)/emailAddress=$(EA)"

	# Sign client CSR.
	openssl x509 -req -days 730 -in $(CRT_LOCATION)/$(CRT_FILE_NAME).csr -CA $(CRT_LOCATION)/ca.crt -CAkey $(CRT_LOCATION)/ca.key -CAcreateserial -out $(CRT_LOCATION)/$(CRT_FILE_NAME).crt

	# Remove CSR.
	rm $(CRT_LOCATION)/$(CRT_FILE_NAME).csr

client_cert_revoked:
	# Create magistrala server key and CSR.
	$(eval COMMON_NAME=$(CLINET_CN))
	$(file >> $(CRT_LOCATION)/client.cnf,$(CERT_CONFIG))

	openssl req -new -sha256 -newkey rsa:4096 -nodes \
				-keyout $(CRT_LOCATION)/$(CRT_FILE_NAME)_revoked.key \
				-out $(CRT_LOCATION)/$(CRT_FILE_NAME)_revoked.csr \
				-config $(CRT_LOCATION)/client.cnf \
				-extensions v3_req


	# Sign client CSR.
	openssl x509 -req -days 730 -in $(CRT_LOCATION)/$(CRT_FILE_NAME)_revoked.csr \
				-CA $(CRT_LOCATION)/ca.crt \
				-CAkey $(CRT_LOCATION)/ca.key \
				-CAcreateserial \
				-out $(CRT_LOCATION)/$(CRT_FILE_NAME)_revoked.crt \
				-extfile $(CRT_LOCATION)/client.cnf \
				-extensions v3_req

	$(file > $(CRT_LOCATION)/ca.cnf,$(REVOKE_CONFIG) )
	@touch $(CRT_LOCATION)/index.txt
	@echo "01" > $(CRT_LOCATION)/crlnumber
	openssl ca -config $(CRT_LOCATION)/ca.cnf  --cert $(CRT_LOCATION)/ca.crt --keyfile $(CRT_LOCATION)/ca.key -revoke $(CRT_LOCATION)/$(CRT_FILE_NAME)_revoked.crt
	openssl ca -config $(CRT_LOCATION)/ca.cnf  --cert $(CRT_LOCATION)/ca.crt --keyfile $(CRT_LOCATION)/ca.key -gencrl -out $(CRT_LOCATION)/revoked_certs.crl

	# Remove CSR.
	rm $(CRT_LOCATION)/$(CRT_FILE_NAME)_revoked.csr $(CRT_LOCATION)/ca.cnf $(CRT_LOCATION)/client.cnf   $(CRT_LOCATION)/index.txt*  $(CRT_LOCATION)/crlnumber  $(CRT_LOCATION)/crlnumber.old

client_cert_unknown:
	# Create magistrala server key and CSR.
	openssl req -new -sha256 -newkey rsa:4096 -nodes -keyout $(CRT_LOCATION)/$(CRT_FILE_NAME)_unknown.key \
				-out $(CRT_LOCATION)/$(CRT_FILE_NAME)_unknown.csr -subj "/CN=$(CLINET_CN)/O=$(O)/OU=$(OU_CRT)/emailAddress=$(EA)"

	# Sign client CSR.
	openssl x509 -req -days 730 -in $(CRT_LOCATION)/$(CRT_FILE_NAME)_unknown.csr -CA $(CRT_LOCATION)/ca.crt -CAkey $(CRT_LOCATION)/ca.key -CAcreateserial -out $(CRT_LOCATION)/$(CRT_FILE_NAME)_unknown.crt

	# Remove CSR.
	rm $(CRT_LOCATION)/$(CRT_FILE_NAME)_unknown.csr

clean_certs:
	rm -r $(CRT_LOCATION)/*.crt
	rm -r $(CRT_LOCATION)/*.key
	rm -r $(CRT_LOCATION)/*.crl
	rm -r $(CRT_LOCATION)/*.srl

