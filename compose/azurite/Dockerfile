# The service's local object store: Azurite's blob service on 10000. This FROM
# line is Azurite's one pin.
FROM mcr.microsoft.com/azure-storage/azurite:3.37.0

# The image defines no health check; its busybox nc shows the blob listener
# accepts connections.
HEALTHCHECK --start-period=30s --start-interval=1s --interval=5s --timeout=3s --retries=5 \
    CMD ["nc", "-z", "127.0.0.1", "10000"]

# azblob v1.8.1 sends an x-ms-version newer than the image accepts by default,
# so the version check is off.
CMD ["azurite-blob", "--blobHost", "0.0.0.0", "--skipApiVersionCheck", "-l", "/data"]
