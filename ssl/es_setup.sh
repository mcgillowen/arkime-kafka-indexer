#!/usr/bin/env bash

if [ "${ELASTIC_PASSWORD}" == "" ]; then
  echo "Set the ELASTIC_PASSWORD environment variable in the .env file"
  exit 1;
elif [ "${KIBANA_PASSWORD}" == "" ]; then
  echo "Set the KIBANA_PASSWORD environment variable in the .env file"
  exit 1;
fi;
if [ ! -f config/certs/ca.zip ]; then
  echo "Creating CA"
  bin/elasticsearch-certutil ca --silent --pem -out config/certs/ca.zip
  unzip config/certs/ca.zip -d config/certs
fi;
if [ ! -f config/certs/certs.zip ]; then
  KIBANA_IP="$(getent ahosts kibana | grep kibana | awk '{print $1}')"
  ES01_IP="$(getent ahosts es01 | grep es01 | awk '{print $1}')"
  echo "Creating certs"
  echo -ne \
  "instances:\n"\
  "  - name: es01\n"\
  "    dns:\n"\
  "      - es01\n"\
  "      - localhost\n"\
  "    ip:\n"\
  "      - 127.0.0.1\n"\
  "      - $ES01_IP\n"\
  "  - name: kibana\n"\
  "    dns:\n"\
  "      - kibana\n"\
  "      - localhost\n"\
  "    ip:\n"\
  "      - 127.0.0.1\n"\
  "      - $KIBANA_IP\n"\
  > config/certs/instances.yml
  bin/elasticsearch-certutil cert --silent --pem -out config/certs/certs.zip --in config/certs/instances.yml --ca-cert config/certs/ca/ca.crt --ca-key config/certs/ca/ca.key
  unzip config/certs/certs.zip -d config/certs
fi;
echo "Setting file permissions"
chown -R root:root config/certs
find . -type d -exec chmod 750 \{\} \;
find . -type f -exec chmod 640 \{\} \;
echo "Waiting for Elasticsearch availability"
until curl -s --cacert config/certs/ca/ca.crt https://es01:9200 | grep -q "missing authentication credentials"; do 
  sleep 30
done
echo "Setting kibana_system password"
until curl -s -X POST --cacert config/certs/ca/ca.crt -u "elastic:${ELASTIC_PASSWORD}" -H "Content-Type: application/json" https://es01:9200/_security/user/kibana_system/_password -d "{\"password\":\"${KIBANA_PASSWORD}\"}" | grep -q "^{}"; do
  sleep 10
done

echo "All done!";