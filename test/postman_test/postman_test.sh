#!/usr/bin/env bash

newman run "../BCDA_Tests_Sequential.postman_collection.json" -e "../dev.postman_environment.json"