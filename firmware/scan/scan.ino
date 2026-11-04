#include "BluetoothSerial.h"

BluetoothSerial bt;

void setup() {
  Serial.begin(115200);
  bt.begin("pixelhub-bridge", true);
}

void loop() {
  Serial.println("scanning...");
  BTScanResults *results = bt.discover(10000);
  if (results) {
    for (int i = 0; i < results->getCount(); i++) {
      BTAdvertisedDevice *d = results->getDevice(i);
      Serial.printf("%s  rssi=%d  %s\n", d->getAddress().toString().c_str(), d->getRSSI(), d->getName().c_str());
    }
  }
  delay(5000);
}
