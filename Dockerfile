FROM python:3.11-rc-alpine
WORKDIR /app
COPY . /app
ENV GLIBC_REPO=https://github.com/sgerrand/alpine-pkg-glibc
ENV GLIBC_VERSION=2.35-r1

RUN set -ex && \
    apk --update add libstdc++ curl ca-certificates make && \
    for pkg in glibc-${GLIBC_VERSION} glibc-bin-${GLIBC_VERSION}; \
        do curl -sSL ${GLIBC_REPO}/releases/download/${GLIBC_VERSION}/${pkg}.apk -o /tmp/${pkg}.apk; done && \
    apk add --allow-untrusted /tmp/*.apk && \
    rm -v /tmp/*.apk && \
    /usr/glibc-compat/sbin/ldconfig /lib /usr/glibc-compat/lib

RUN pip3 install --no-cache-dir -r requirements.txt

RUN mkdir /tmp/unrar && \
  curl -o \
    /tmp/unrar.tar.gz -L \
    "https://www.rarlab.com/rar/unrarsrc-6.2.6.tar.gz" && \  
    tar xf /tmp/unrar.tar.gz -C /tmp/unrar --strip-components=1 && \
    cd /tmp/unrar && \
    make && \
    install -v -m755 unrar /usr/local/bin



# RUN wget https://www.win-rar.com/fileadmin/winrar-versions/rarlinux-x64-624.tar.gz && \
#     tar -zxvf rarlinux-x64-624.tar.gz && \
#     cp -v ./rar/rar ./rar/unrar /usr/local/bin/ && \
#     rm rarlinux-x64-624.tar.gz && \
#     rm -rf ./rar

EXPOSE 8000
ENV PORT=8000
CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8000"]
