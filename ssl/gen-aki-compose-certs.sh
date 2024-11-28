#!/bin/bash

bash gen-ssl-certs.sh ca ca-cert docker-compose
bash gen-ssl-certs.sh client ca-cert client_indexer_ indexer
bash gen-ssl-certs.sh -k client ca-cert client_ui_ ui
bash gen-ssl-certs.sh -k server ca-cert broker_kafka-3_ kafka-3
bash gen-ssl-certs.sh -k server ca-cert broker_kafka-2_ kafka-2
bash gen-ssl-certs.sh -k server ca-cert broker_kafka-1_ kafka-1
