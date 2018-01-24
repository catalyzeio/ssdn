#!/bin/bash -e
if [ "$SHORT" == "true" ]; then
   export SHORT_TEST=-short
fi
make clean deb
aws s3 cp *.deb s3://paas-assets/latest/ --acl private --region us-east-1
