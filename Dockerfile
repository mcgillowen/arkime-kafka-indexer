FROM --platform=$BUILDPLATFORM gcr.io/distroless/base-debian11:latest
ENTRYPOINT [ "/arkime-kafka-indexer" ]
COPY arkime-kafka-indexer /arkime-kafka-indexer
